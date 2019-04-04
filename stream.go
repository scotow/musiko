package musiko

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/grafov/m3u8"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	playlistCapacity = 256
	playlistSize     = 6  // About 60 sec of music.
	fetchLimit       = 64 // About 10 min of music.
)

const (
	stopped = iota
	running
	paused
	killed
)

var (
	ErrStreamAlreadyStarted = errors.New("stream cannot be started")
	ErrStreamNotRunning     = errors.New("stream not running")
	ErrStreamNotPaused      = errors.New("stream not paused")
	ErrPlaylistEmpty        = errors.New("the playlist is empty")
	ErrInvalidPlaylistEntry = errors.New("playlist contains a nil entry")
	ErrPartNotFound         = errors.New("part not found")
)

type PartURIModifier func(string) string

func NewStream(client *Client, station string, proxyLess bool) (*Stream, error) {
	stream := new(Stream)

	playlist, err := m3u8.NewMediaPlaylist(playlistSize, playlistCapacity)
	if err != nil {
		return nil, err
	}
	stream.playlist = playlist

	stream.id = uuid.New()
	stream.client = client
	stream.parts = make(map[string]*Part)
	stream.station = station

	if proxyLess {
		stream.httpClient = httpClientNoProxy()
	} else {
		stream.httpClient = http.DefaultClient
	}

	return stream, nil
}

func httpClientNoProxy() *http.Client {
	var defaultTransport http.RoundTripper = &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          30,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{Transport: defaultTransport}
}

type Stream struct {
	id      uuid.UUID
	state   uint
	station string

	httpClient *http.Client
	client     *Client

	errChan    chan<- error
	pauseChan  chan struct{}
	resumeChan chan struct{}

	queue    []*m3u8.MediaSegment
	parts    map[string]*Part
	playlist *m3u8.MediaPlaylist

	URIModifier PartURIModifier

	fetching bool
	sync.RWMutex
}

func (s *Stream) Start(report chan<- error) error {
	if s.state != stopped {
		return ErrStreamAlreadyStarted
	}

	log.Printf("Starting stream (%s).\n", s.id.String())

	if s.shouldFetchPlaylist() {
		err := s.queueNextPlaylist()
		if err != nil {
			return err
		}
	}

	s.errChan = report
	s.pauseChan = make(chan struct{})
	s.resumeChan = make(chan struct{})

	go s.queueLoop()
	s.state = running

	log.Printf("Stream started (%s).\n", s.id.String())
	return nil
}

func (s *Stream) Pause() error {
	if s.state != running {
		return ErrStreamNotRunning
	}

	s.pauseChan <- struct{}{}
	s.state = paused

	log.Printf("Stream paused (%s).\n", s.id.String())
	return nil
}

func (s *Stream) Resume() error {
	if s.state != paused {
		return ErrStreamNotPaused
	}

	s.resumeChan <- struct{}{}
	s.state = running

	log.Printf("Stream resumed (%s).\n", s.id.String())
	return nil
}

func (s *Stream) shouldFetchPlaylist() bool {
	s.RLock()
	defer s.RUnlock()

	return len(s.queue) <= fetchLimit && !s.fetching
}

func (s *Stream) queueNextPlaylist() error {
	s.Lock()
	s.fetching = true
	s.Unlock()

	// Unlock fetching on exit.
	defer func() {
		s.Lock()
		s.fetching = false
		s.Unlock()
	}()

	log.Printf("Queuing a new playlist (%s).\n", s.id.String())

	tracks, err := s.client.HighQualityTracks(s.station, s.httpClient)
	if err != nil {
		return err
	}

	var (
		wg        sync.WaitGroup
		errCommon error
	)
	wg.Add(len(tracks))

	for _, t := range tracks {
		go func(track *Track) {
			defer wg.Done()

			playlist, parts, err := track.GetParts()
			if err != nil {
				errCommon = err
				return
			}

			// Remove reference to the original data to allow the GC to free it.
			track.ClearData()

			log.Printf("Track fetched and split (%s).\n", track.id.String())

			s.Lock()
			defer s.Unlock()

			for i, seg := range playlist.Segments {
				if seg == nil {
					break
				}

				err := s.playlist.AppendSegment(seg)
				if err != nil {
					errCommon = err
					return
				}

				// Set Discontinuity tag for the first part.
				if i == 0 {
					err = s.playlist.SetDiscontinuity()
					if err != nil {
						errCommon = err
						return
					}
				}

				// Apply URI modifier if required.
				if s.URIModifier != nil {
					seg.URI = s.URIModifier(seg.URI)
				}

				// Store the part in the main lookup map and to the auto-remove queue.
				s.parts[seg.URI] = parts[i]
				s.queue = append(s.queue, seg)
			}

			log.Printf("Track added to main playlist (%s).\n", track.id.String())
		}(t)
	}

	wg.Wait()

	log.Printf("New playlist queued (%s).\n", s.id.String())
	return errCommon
}

func (s *Stream) queueLoop() {
	fetchFailed := make(chan struct{})

	for {
		s.RLock()

		// TODO: Cancel/Wait for new parts rather than 'throwing'.
		if len(s.queue) == 0 {
			s.RUnlock()
			s.globalError(ErrPlaylistEmpty)
			return
		}

		part := s.queue[0]
		if part == nil {
			s.RUnlock()
			s.globalError(ErrInvalidPlaylistEntry)
			return
		}
		s.RUnlock()

		// TODO: Use time difference for removal.
		// Wait for chunk to be played or stop message.
		select {
		case <-time.After(time.Duration(part.Duration * float64(time.Second))):
		case <-s.pauseChan:
			<-s.resumeChan
		case <-fetchFailed:
			return
		}

		s.Lock()
		err := s.playlist.Remove()
		if err != nil {
			s.Unlock()
			s.globalError(err)
			return
		}

		// Shift removed part and remove it from part map.
		s.queue[0] = nil
		s.queue = s.queue[1:]
		delete(s.parts, part.URI)

		s.Unlock()

		// TODO: Use song duration rather than part count.
		if s.shouldFetchPlaylist() {
			log.Printf("Playlist almost empty: %d (%s).\n", len(s.queue), s.id.String())
			go func() {
				err := s.queueNextPlaylist()
				if err != nil {
					s.globalError(err)
					fetchFailed <- struct{}{}
				}
			}()
		}
	}
}

func (s *Stream) globalError(err error) {
	s.state = killed
	if s.errChan != nil {
		s.errChan <- err
	}
}

func (s *Stream) WritePlaylist(writer io.Writer) error {
	s.RLock()

	// TODO: Use a inner cache.
	// Copy playlist data to a temporary buffer because the writer can be slow.
	buffer := s.playlist.Encode().Bytes()
	data := make([]byte, len(buffer))
	copy(data, buffer)

	// Unlock here to allow long writing.
	s.RUnlock()

	_, err := writer.Write(data)
	return err
}

func (s *Stream) getPart(name string) (*Part, error) {
	s.RLock()
	defer s.RUnlock()

	// Because we never alter the part's data we don't need to make a copy before writing.
	part, exists := s.parts[name]
	if !exists {
		return nil, ErrPartNotFound
	}

	return part, nil
}

func (s *Stream) WritePartData(writer io.Writer, name string) error {
	part, err := s.getPart(name)
	if err != nil {
		return err
	}

	_, err = writer.Write(part.Data)
	return err
}

func (s *Stream) WriteInfo(writer io.Writer, name string) error {
	part, err := s.getPart(name)
	if err != nil {
		return err
	}

	infoJson, err := json.Marshal(part.Info)
	if err != nil {
		return err
	}

	_, err = writer.Write(infoJson)
	return err
}

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
	playlistCapacity = 256     // About 10 songs.
	playlistSize     = 6       // About 60 sec of music.
	fetchLimit       = 10 * 60 // 10 min of music.
)

// Stream state.
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
	ErrTrackNotClear        = errors.New("track not properly cleared")
	ErrTrackNotFound        = errors.New("track not found")
	ErrPartNotFound         = errors.New("part not found")
)

type PartURIModifier func(string, int) string

func NewStream(client *Client, station string, proxyLess bool) (*Stream, error) {
	stream := new(Stream)

	playlist, err := m3u8.NewMediaPlaylist(playlistSize, playlistCapacity)
	if err != nil {
		return nil, err
	}

	stream.id = uuid.New()
	stream.station = station
	stream.client = client
	stream.playlist = playlist

	stream.queue = make([]*Track, 0)
	stream.tracks = make(map[string]*Track)

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

	available float64
	queue     []*Track
	tracks    map[string]*Track
	playlist  *m3u8.MediaPlaylist

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

	return s.available <= fetchLimit && !s.fetching
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

			playlist, _, err := track.GetParts()
			if err != nil {
				errCommon = err
				return
			}

			log.Printf("Track fetched and split (%s).\n", track.id.String())

			s.Lock()
			defer s.Unlock()

			s.queue = append(s.queue, track)
			s.tracks[track.id.String()] = track

			for i, seg := range playlist.Segments {
				if seg == nil {
					break
				}

				// Increment total duration by segment duration.
				s.available += seg.Duration

				// Apply URI modifier if required.
				if s.URIModifier != nil {
					seg.URI = s.URIModifier(track.id.String(), i)
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
			}

			log.Printf("Track added to main playlist (%s).\n", track.id.String())
		}(t)
	}

	wg.Wait()

	log.Printf("New playlist queued. Total duration: %.2fs (%s).\n", s.available, s.id.String())
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

		track := s.queue[0]
		if len(track.queue) == 0 {
			s.RUnlock()
			s.globalError(ErrTrackNotClear)
			return
		}

		part := track.queue[0]
		s.RUnlock()

		// TODO: Use time difference for removal.
		// Wait for chunk to be played or stop message.
		select {
		case <-time.After(time.Duration(part.seg.Duration * float64(time.Second))):
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

		// If track is empty, remove it from the map and queue.
		if track.slide() {
			s.queue = s.queue[1:]
			delete(s.tracks, track.id.String())
		}

		// Remove segment duration from the total.
		s.available -= part.seg.Duration

		s.Unlock()

		if s.shouldFetchPlaylist() {
			log.Printf("Playlist almost empty: %.2fs (%s).\n", s.available, s.id.String())
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

func (s *Stream) WritePlaylist(writer io.Writer) (int, error) {
	s.RLock()

	// TODO: Use a inner cache.
	// Copy playlist data to a temporary buffer because the writer can be slow.
	buffer := s.playlist.Encode().Bytes()
	data := make([]byte, len(buffer))
	copy(data, buffer)

	// Unlock here to allow long writing.
	s.RUnlock()

	return writer.Write(data)
}

func (s *Stream) getPart(trackId string, index int) (*Part, error) {
	s.RLock()
	defer s.RUnlock()

	// Because we never alter the part's data we don't need to make a copy before writing.
	track, exists := s.tracks[trackId]
	if !exists {
		return nil, ErrPartNotFound
	}

	if index < 0 || index >= len(track.parts) {
		return nil, ErrPartNotFound
	}

	return track.parts[index], nil
}

func (s *Stream) WritePartData(writer io.Writer, trackId string, index int) (int, error) {
	part, err := s.getPart(trackId, index)
	if err != nil {
		return 0, err
	}

	return writer.Write(part.data)
}

func (s *Stream) WriteInfo(writer io.Writer, trackId string) (int, error) {
	s.RLock()
	defer s.RUnlock()

	track, exists := s.tracks[trackId]
	if !exists {
		return 0, ErrTrackNotFound
	}

	infoJson, err := json.Marshal(track.info)
	if err != nil {
		return 0, err
	}

	return writer.Write(infoJson)
}

func (s *Stream) TrackInfo(trackId string) (*TrackInfo, error) {
	s.RLock()
	defer s.RUnlock()

	track, exists := s.tracks[trackId]
	if !exists {
		return nil, ErrTrackNotFound
	}

	return &track.info, nil
}

func (s *Stream) WriteTrack(writer io.Writer, trackId string) (int, error) {
	s.RLock()

	// Because we never alter the track's data we don't need to make a copy before writing.
	track, exists := s.tracks[trackId]
	if !exists {
		return 0, ErrTrackNotFound
	}

	s.RUnlock()

	return writer.Write(track.data)
}

func (s *Stream) TrackAvailable(trackId string) bool {
	s.RLock()
	defer s.RUnlock()

	_, exists := s.tracks[trackId]
	return exists
}

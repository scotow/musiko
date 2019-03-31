package musiko

import (
	"errors"
	"fmt"
	"github.com/cellofellow/gopiano"
	"github.com/cellofellow/gopiano/responses"
	"github.com/grafov/m3u8"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	playlistSize = 6
	fetchLimit   = 64
)

var (
	//ErrPartsDirNotFound     = errors.New("parts directory doesn't exist")
	ErrNoTracksFound        = errors.New("no tracks found")
	ErrPlaylistEmpty        = errors.New("the playlist is empty")
	ErrInvalidPlaylistEntry = errors.New("playlist contains a nil entry")
	ErrPartNotFound         = errors.New("part not found")
)

var (
	qualitiesOrder = []string{"high", "medium", "low"}
)

func init() {
	for i, q := range qualitiesOrder {
		qualitiesOrder[i] = fmt.Sprintf("%sQuality", q)
	}
}

type Credentials struct {
	Username string
	Password string
}

func NewStream(cred Credentials, station string, proxyless bool) (*Stream, error) {
	stream := new(Stream)

	if proxyless {
		stream.httpClient = httpClientNoProxy()
	} else {
		stream.httpClient = http.DefaultClient
	}

	client, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		return nil, err
	}
	stream.client = client
	stream.cred = cred

	err = stream.authClient()
	if err != nil {
		return nil, err
	}

	playlist, err := m3u8.NewMediaPlaylist(playlistSize, 256)
	if err != nil {
		return nil, err
	}
	stream.playlist = playlist
	stream.parts = make(map[string][]byte)
	stream.station = station

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
	//running  bool
	station string

	httpClient *http.Client
	client     *gopiano.Client
	cred       Credentials

	errChan    chan<- error
	pauseChan  chan struct{}
	resumeChan chan struct{}

	queue    []*m3u8.MediaSegment
	parts    map[string][]byte
	playlist *m3u8.MediaPlaylist

	fetching bool
	sync.RWMutex
}

func (s *Stream) authClient() error {
	_, err := s.client.AuthPartnerLogin()
	if err != nil {
		return err
	}

	_, err = s.client.AuthUserLogin(s.cred.Username, s.cred.Password)
	if err != nil {
		return err
	}

	return nil
}

func (s *Stream) highQualityTracks() ([]*Track, error) {
	resp, err := s.client.StationGetPlaylist(s.station)
	if err != nil {
		// Check if the error is a 'INVALID_AUTH_TOKEN' error, aka. 'token expired'.
		if pErr, is := err.(responses.ErrorResponse); is && pErr.Code == 1001 {
			err = s.authClient()
			if err != nil {
				return nil, err
			}

			// Retry playlist fetch.
			resp, err = s.client.StationGetPlaylist(s.station)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	tracks := make([]*Track, 0, len(resp.Result.Items))
	for _, item := range resp.Result.Items {
		for _, quality := range qualitiesOrder {
			if item, exists := item.AudioURLMap[quality]; exists {
				tracks = append(tracks, NewTrack(item.AudioURL, s.httpClient))
				break
			}
		}
	}

	if len(tracks) == 0 {
		return nil, ErrNoTracksFound
	}

	return tracks, nil
}

func (s *Stream) Start() (<-chan error, error) {
	log.Println("Starting stream...")

	errChan := make(chan error)
	s.errChan = errChan

	if s.shouldFetchPlaylist() {
		err := s.queueNextPlaylist()
		if err != nil {
			return nil, err
		}
	}

	s.pauseChan = make(chan struct{})
	s.resumeChan = make(chan struct{})

	go s.autoRemove()

	log.Println("Stream started.")
	return errChan, nil
}

func (s *Stream) Pause() {
	s.pauseChan <- struct{}{}
	log.Println("Stream paused.")
}

func (s *Stream) Resume() {
	s.resumeChan <- struct{}{}
	log.Println("Stream resumed.")
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

	log.Println("Queuing a new playlist.")

	tracks, err := s.highQualityTracks()
	if err != nil {
		return err
	}

	var (
		wg        sync.WaitGroup
		errCommon error
	)
	wg.Add(len(tracks))

	for _, track := range tracks {
		go func(t *Track) {
			defer wg.Done()

			playlist, parts, err := t.GetParts()
			if err != nil {
				errCommon = err
				return
			}

			log.Printf("Track fetched and split (%s).\n", t.id.String())

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

				// Set Discontinuity tag for the new part.
				if i == 0 {
					err = s.playlist.SetDiscontinuity()
					if err != nil {
						errCommon = err
						return
					}
				}

				s.parts[seg.URI] = parts[i]
				s.queue = append(s.queue, seg)
			}

			log.Printf("Track added to main playlist (%s).\n", track.id.String())
		}(track)
	}

	wg.Wait()

	s.Lock()
	s.fetching = false
	s.Unlock()

	log.Println("New playlist queued.")
	return errCommon
}

func (s *Stream) autoRemove() {
	for {
		s.RLock()
		if len(s.queue) == 0 {
			s.RUnlock()
			s.errChan <- ErrPlaylistEmpty
			return
		}

		part := s.queue[0]
		if part == nil {
			s.RUnlock()
			s.errChan <- ErrInvalidPlaylistEntry
			return
		}
		s.RUnlock()

		// TODO: Use time difference for removal.
		// Wait for chunk to be played or stop message.
		select {
		case <-time.After(time.Duration(part.Duration * float64(time.Second))):
		case <-s.pauseChan:
			<-s.resumeChan
		}

		s.Lock()
		err := s.playlist.Remove()
		if err != nil {
			s.Unlock()
			s.errChan <- err
			return
		}

		// Shift removed part and remove it from part map.
		s.queue[0] = nil
		s.queue = s.queue[1:]
		delete(s.parts, part.URI)

		s.Unlock()

		// TODO: Use song duration rather than part count.
		if s.shouldFetchPlaylist() {
			log.Printf("Playlist almost empty (%d).\n", len(s.queue))
			go func() {
				err := s.queueNextPlaylist()
				if err != nil {
					s.errChan <- err
				}
			}()
		}
	}
}

func (s *Stream) WritePlaylist(writer io.Writer) error {
	s.RLock()

	// TODO: Use a inner cache.
	// Copy playlist data to a temporary buffer.
	buffer := s.playlist.Encode().Bytes()
	data := make([]byte, len(buffer))
	copy(data, buffer)

	s.RUnlock()

	_, err := writer.Write(data)
	return err
}

func (s *Stream) WritePart(writer io.Writer, name string) error {
	s.RLock()

	part, exists := s.parts[name]
	if !exists {
		return ErrPartNotFound
	}

	// Unlock here to allow long writing.
	s.RUnlock()

	_, err := writer.Write(part)
	return err
}

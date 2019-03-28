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
	fetchLimit   = 50
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

func NewStream(cred Credentials /*partsDir string,*/, proxyless bool) (*Stream, error) {
	stream := new(Stream)

	/*_, err := os.Stat(partsDir)
	if os.IsNotExist(err) {
		return nil, ErrPartsDirNotFound
	}
	stream.partsDir = partsDir*/

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
	//partsDir string
	//running  bool
	station string

	httpClient *http.Client
	client     *gopiano.Client
	cred       Credentials

	errChan chan<- error

	//tracks   []*Track
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

/*func (s *Stream) nextTrack() (*Track, error) {
	s.Lock()

	if len(s.tracks) == 0 {
		tracks, err := s.highQualityTracks()
		if err != nil {
			return nil, err
		}

		s.tracks = tracks
	}

	track := s.tracks[0]
	s.tracks = s.tracks[1:]

	s.Unlock()

	return track, nil
}

func (s *Stream) queueNextTrack() error {
	next, err := s.nextTrack()
	if err != nil {
		return err
	}

	playlist, err := next.SplitTS(s.partsDir, false, s.httpClient)

	s.Lock()
	defer s.Unlock()

	for i, part := range playlist.Segments {
		if part == nil {
			break
		}

		err := s.playlist.AppendSegment(part)
		if err != nil {
			return err
		}

		// Set Discontinuity tag for the new part.
		if i == 0 {
			err = s.playlist.SetDiscontinuity()
			if err != nil {
				return err
			}
		}

		// Append the part to the list.
		s.parts = append(s.parts, part)
	}

	return nil
}*/

func (s *Stream) Start(station string) (error, <-chan error) {
	log.Println("Starting stream...")

	playlist, err := m3u8.NewMediaPlaylist(playlistSize, 1024)
	if err != nil {
		return err, nil
	}

	errChan := make(chan error)

	s.station = station
	s.playlist = playlist
	s.parts = make(map[string][]byte)
	s.queue = nil
	s.errChan = errChan

	err = s.queueNextPlaylist()
	if err != nil {
		return err, nil
	}

	go s.autoRemove()

	log.Println("Stream started.")
	return nil, errChan
}

func (s *Stream) Stop() error {
	return nil
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
					return
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
		s.Lock()
		if len(s.queue) == 0 {
			s.Unlock()
			s.errChan <- ErrPlaylistEmpty
			return
		}

		part := s.queue[0]
		if part == nil {
			s.Unlock()
			s.errChan <- ErrInvalidPlaylistEntry
			return
		}
		s.Unlock()

		// TODO: Use time difference for removal.
		// Wait for chunk to be played.
		time.Sleep(time.Duration(part.Duration * float64(time.Second)))

		s.Lock()
		err := s.playlist.Remove()
		if err != nil {
			s.Unlock()
			s.errChan <- err
			return
		}

		// Remove part from disk.
		/*err = os.Remove(path.Join(s.partsDir, part.URI))
		if err != nil {
			s.Unlock()
			s.errChan <- err
			return
		}*/

		// Shift removed part and remove it from part map.
		s.queue = s.queue[1:]
		delete(s.parts, part.URI)

		//log.Println("Part removed from playlist.")

		// TODO: Use song duration rather than part count.
		if len(s.queue) == fetchLimit && !s.fetching {
			log.Printf("Playlist almost empty (%d).\n", len(s.queue))
			go func() {
				err := s.queueNextPlaylist()
				if err != nil {
					s.errChan <- err
				}
			}()
		}

		s.Unlock()
	}
}

// TODO: Can this block the stream for too long?
func (s *Stream) WritePlaylist(writer io.Writer) error {
	s.RLock()
	defer s.RUnlock()

	_, err := io.Copy(writer, s.playlist.Encode())
	return err
}

func (s *Stream) WritePart(writer io.Writer, name string) error {
	s.RLock()
	defer s.RUnlock()

	part, exists := s.parts[name]
	if !exists {
		return ErrPartNotFound
	}

	_, err := writer.Write(part)
	return err
}

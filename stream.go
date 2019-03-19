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
	"os"
	"path"
	"sync"
	"time"
)

const (
	playlistSize = 6
)

var (
	ErrPartsDirNotFound = errors.New("parts directory doesn't exist")
	ErrNoTracksFound    = errors.New("no tracks found")
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

func NewStream(cred Credentials, partsDir string, proxyless bool) (*Stream, error) {
	stream := new(Stream)

	_, err := os.Stat(partsDir)
	if os.IsNotExist(err) {
		return nil, ErrPartsDirNotFound
	}
	stream.partsDir = partsDir

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
	partsDir string
	running  bool
	station  string

	httpClient *http.Client
	client     *gopiano.Client
	cred       Credentials

	tracks   []*Track
	parts    []*m3u8.MediaSegment
	playlist *m3u8.MediaPlaylist

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
				tracks = append(tracks, NewTrack(item.AudioURL))
				break
			}
		}
	}

	if len(tracks) == 0 {
		return nil, ErrNoTracksFound
	}

	return tracks, nil
}

func (s *Stream) nextTrack() (*Track, error) {
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

	s.Unlock()

	return nil
}

func (s *Stream) Start(station string) error {
	s.Lock()

	playlist, err := m3u8.NewMediaPlaylist(playlistSize, 128)
	if err != nil {
		return err
	}

	s.station = station
	s.playlist = playlist
	s.tracks = nil
	s.parts = nil

	s.Unlock()

	err = s.queueNextTrack()
	if err != nil {
		return err
	}

	go s.autoRemove()

	return nil
}

func (s *Stream) Stop() error {
	s.Lock()
	for _, part := range s.parts {
		err := os.Remove(path.Join(s.partsDir, part.URI))
		if err != nil {
			return err
		}
	}
	s.Unlock()

	return nil
}

// TODO: Handle error with a chan<error>.
func (s *Stream) autoRemove() {
	for {
		s.Lock()
		part := s.parts[0]
		if part == nil {
			log.Fatalln("no part in playlist")
			break
		}
		s.Unlock()

		// TODO: Use time difference for removal.
		// Wait for chunk to be played.
		time.Sleep(time.Duration(part.Duration * float64(time.Second)))

		s.Lock()
		err := s.playlist.Remove()
		if err != nil {
			log.Fatalln("cannot remove part", err)
			break
		}

		// Remove part from disk.
		err = os.Remove(path.Join(s.partsDir, part.URI))
		if err != nil {
			log.Fatalln("cannot queue new track", err)
			break
		}

		// Shift removed part.
		s.parts = s.parts[1:]
		s.Unlock()

		if s.playlist.Count() <= playlistSize {
			err = s.queueNextTrack()
			if err != nil {
				log.Fatalln("cannot queue new track", err)
				break
			}
		}
	}
}

func (s *Stream) WritePlaylist(writer io.Writer) error {
	s.RLock()
	_, err := io.Copy(writer, s.playlist.Encode())
	s.RUnlock()

	return err
}

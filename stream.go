package musiko

import (
	"errors"
	"fmt"
	"github.com/cellofellow/gopiano"
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

func buildClient(cred Credentials) (*gopiano.Client, error) {
	client, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		return nil, err
	}

	_, err = client.AuthPartnerLogin()
	if err != nil {
		return nil, err
	}

	_, err = client.AuthUserLogin(cred.Username, cred.Password)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func NewStream(cred Credentials, partsDir string, proxyless bool) (*Stream, error) {
	_, err := os.Stat(partsDir)
	if os.IsNotExist(err) {
		return nil, ErrPartsDirNotFound
	}

	var httpClient *http.Client
	if proxyless {
		httpClient = httpClientNoProxy()
	} else {
		httpClient = http.DefaultClient
	}

	client, err := buildClient(cred)
	if err != nil {
		return nil, err
	}

	return &Stream{
		partsDir:   partsDir,
		client:     client,
		httpClient: httpClient,
	}, nil
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

	playlist     *m3u8.MediaPlaylist
	playlistLock sync.Mutex

	tracks     []*Track
	parts      []string
	tracksLock sync.Mutex
}

func (s *Stream) highQualityTracks() ([]*Track, error) {
	resp, err := s.client.StationGetPlaylist(s.station)
	if err != nil {
		return nil, err
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
	s.tracksLock.Lock()

	if len(s.tracks) == 0 {
		tracks, err := s.highQualityTracks()
		if err != nil {
			return nil, err
		}

		s.tracks = tracks
	}

	track := s.tracks[0]
	s.tracks = s.tracks[1:]

	s.tracksLock.Unlock()

	return track, nil
}

func (s *Stream) queueNextTrack() error {
	next, err := s.nextTrack()
	if err != nil {
		return err
	}

	playlist, err := next.SplitTS(s.partsDir, false, s.httpClient)

	s.playlistLock.Lock()
	for _, part := range playlist.Segments {
		if part == nil {
			break
		}

		err := s.playlist.AppendSegment(part)
		if err != nil {
			return err
		}
		s.parts = append(s.parts, part.URI)
	}
	s.playlistLock.Unlock()

	return nil
}

func (s *Stream) Start(station string) error {
	playlist, err := m3u8.NewMediaPlaylist(6, 128)
	if err != nil {
		return err
	}

	s.station = station
	s.playlist = playlist

	err = s.queueNextTrack()
	if err != nil {
		return err
	}

	go s.autoRemove()

	return nil
}

func (s *Stream) Stop() error {
	s.tracksLock.Lock()
	for _, part := range s.parts {
		err := os.Remove(path.Join(s.partsDir, part))
		if err != nil {
			return err
		}
	}
	s.tracksLock.Unlock()

	return nil
}

//TODO: Handle error with a chan<error>.
func (s *Stream) autoRemove() {
	for {
		s.playlistLock.Lock()
		part := s.playlist.Segments[0]
		if part == nil {
			log.Fatalln("no part in playlist")
			break
		}
		s.playlistLock.Unlock()

		time.Sleep(time.Duration(part.Duration * float64(time.Second)))

		s.playlistLock.Lock()
		err := s.playlist.Remove()
		if err != nil {
			log.Fatalln("cannot remove part", err)
			break
		}

		//TODO: Find a better way to delete parts.
		partPath := path.Join(s.partsDir, s.parts[0])
		s.tracksLock.Lock()
		err = os.Remove(partPath)
		if err != nil {
			log.Fatalln("cannot queue new track", err)
			break
		}
		s.parts = s.parts[1:]
		s.tracksLock.Unlock()
		s.playlistLock.Unlock()

		if s.playlist.Count() < 6 {
			err = s.queueNextTrack()
			if err != nil {
				log.Fatalln("cannot queue new track", err)
				break
			}
		}
	}
}

func (s *Stream) WritePlaylist(writer io.Writer) error {
	s.playlistLock.Lock()
	_, err := io.Copy(writer, s.playlist.Encode())
	s.playlistLock.Unlock()

	return err
}

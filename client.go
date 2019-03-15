package musiko

import (
	"fmt"
	"github.com/cellofellow/gopiano"
	"github.com/pkg/errors"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	ErrNoTracksFound = errors.New("no tracks found")
)

var (
	qualitiesOrder = []string{"high", "medium", "low"}
)

func init() {
	for i, q := range qualitiesOrder {
		qualitiesOrder[i] = fmt.Sprintf("%sQuality", q)
	}
}

func NewClient(username, password string, station string) (*Client, error) {
	client, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		return nil, err
	}

	_, err = client.AuthPartnerLogin()
	if err != nil {
		return nil, err
	}

	_, err = client.AuthUserLogin(username, password)
	if err != nil {
		return nil, err
	}

	return &Client{
		client:     client,
		station:    station,
		HttpClient: httpClientNoProxy(),
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

type Client struct {
	client  *gopiano.Client
	station string

	HttpClient *http.Client

	tracks []string
	lock   sync.Mutex
}

func (c *Client) HighQualityTracks() ([]string, error) {
	resp, err := c.client.StationGetPlaylist(c.station)
	if err != nil {
		return nil, err
	}

	tracks := make([]string, 0, len(resp.Result.Items))
	for _, item := range resp.Result.Items {
		for _, quality := range qualitiesOrder {
			if item, exists := item.AudioURLMap[quality]; exists {
				tracks = append(tracks, item.AudioURL)
				break
			}
		}
	}

	if len(tracks) == 0 {
		return nil, ErrNoTracksFound
	}

	return tracks, nil
}

func (c *Client) NextTrack() (string, error) {
	c.lock.Lock()

	if len(c.tracks) == 0 {
		urls, err := c.HighQualityTracks()
		if err != nil {
			return "", err
		}

		c.tracks = urls
	}

	track := c.tracks[0]
	c.tracks = c.tracks[1:]

	c.lock.Unlock()

	return track, nil
}

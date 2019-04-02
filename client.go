package musiko

import (
	"fmt"
	"github.com/cellofellow/gopiano"
	"github.com/cellofellow/gopiano/responses"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"net/http"
	"sync"
)

var (
	ErrCannotListen       = errors.New("client cannot listen to music")
	ErrCannotCastResponse = errors.New("cannot cast API response")
	ErrNoTracksFound      = errors.New("no tracks found")
)

var (
	qualitiesOrder = []string{"high", "medium", "low"}
)

func init() {
	for i, q := range qualitiesOrder {
		qualitiesOrder[i] = fmt.Sprintf("%sQuality", q)
	}
}

type Request = func() (interface{}, error)

type Credentials struct {
	Username string
	Password string
}

type Client struct {
	httpClient *http.Client
	client     *gopiano.Client
	cred       Credentials
	sync.Mutex
}

func NewClient(credentials Credentials) (*Client, error) {
	client := new(Client)

	pandora, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		return nil, err
	}
	client.client = pandora
	client.cred = credentials

	err = client.Auth()
	if err != nil {
		return nil, err
	}

	return client, nil
}

func CreateClient() (*Client, error) {
	client := new(Client)

	pandora, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		return nil, err
	}
	client.client = pandora

	client.cred = Credentials{
		Username: fmt.Sprintf("%s@gmail.com", uuid.New().String()),
		Password: uuid.New().String(),
	}

	resp, err := pandora.UserCreateUser(
		client.cred.Username, client.cred.Password,
		"Male",
		"US", 10001,
		1980, false,
	)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	if !resp.Result.CanListen {
		return nil, ErrCannotListen
	}

	return client, nil
}

func (c *Client) Auth() error {
	c.Lock()
	defer c.Unlock()

	return c.auth()
}

func (c *Client) auth() error {
	_, err := c.client.AuthPartnerLogin()
	if err != nil {
		return err
	}

	resp, err := c.client.AuthUserLogin(c.cred.Username, c.cred.Password)
	if err != nil {
		return err
	}

	if !resp.Result.CanListen {
		return ErrCannotListen
	}

	return nil
}

func (c *Client) doRequest(request Request) (interface{}, error) {
	c.Lock()
	defer c.Unlock()

	resp, err := request()
	if err != nil {
		// Check if the error is a 'INVALID_AUTH_TOKEN' error, aka. 'token expired'.
		if pErr, is := err.(responses.ErrorResponse); is && pErr.Code == 1001 {
			err = c.auth()
			if err != nil {
				return nil, err
			}

			// Retry the request.
			resp, err = request()
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return resp, nil
}

func (c *Client) GetOrCreateStation(genre string) (string, error) {
	it, err := c.doRequest(func() (interface{}, error) {
		return c.client.StationCreateStationMusic(genre)
	})
	if err != nil {
		return "", err
	}

	resp, ok := it.(*responses.StationCreateStation)
	if !ok {
		return "", ErrCannotCastResponse
	}

	return resp.Result.StationID, nil
}

func (c *Client) HighQualityTracks(station string, httpClient *http.Client) ([]*Track, error) {
	it, err := c.doRequest(func() (interface{}, error) {
		return c.client.StationGetPlaylist(station)
	})
	if err != nil {
		return nil, err
	}

	resp, ok := it.(*responses.StationGetPlaylist)
	if !ok {
		return nil, ErrCannotCastResponse
	}

	tracks := make([]*Track, 0, len(resp.Result.Items))
	for _, item := range resp.Result.Items {
		for _, quality := range qualitiesOrder {
			if item, exists := item.AudioURLMap[quality]; exists {
				tracks = append(tracks, NewTrack(item.AudioURL, httpClient))
				break
			}
		}
	}

	if len(tracks) == 0 {
		return nil, ErrNoTracksFound
	}

	return tracks, nil
}

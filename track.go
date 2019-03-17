package musiko

import (
	"github.com/grafov/m3u8"
	"github.com/pkg/errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
)

var (
	ErrAPIStatusCode     = errors.New("api responded with a non 200")
	ErrWrongPlaylistType = errors.New("ffmpeg returns invalid m3u8 file")
)

func NewTrack(url string) *Track {
	return &Track{
		url: url,
	}
}

type Track struct {
	url string

	playlist *m3u8.MediaPlaylist
}

func (t *Track) Open(httpClient *http.Client) (io.Reader, error) {
	resp, err := httpClient.Get(t.url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, ErrAPIStatusCode
	}

	return resp.Body, nil
}

func (t *Track) GetData(httpClient *http.Client) ([]byte, error) {
	r, err := t.Open(httpClient)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (t *Track) SplitTS(dest string, keepM3u8 bool, httpClient *http.Client) (*m3u8.MediaPlaylist, error) {
	if t.playlist != nil {
		return t.playlist, nil
	}

	r, err := t.Open(httpClient)
	if err != nil {
		return nil, err
	}

	playlistPath, err := FfmpegSplitTS(r, dest)
	if err != nil {
		return nil, err
	}

	playlistFile, err := os.Open(playlistPath)
	if err != nil {
		return nil, err
	}

	// Parse m3u8 file generated by ffmpeg.
	playlist, playlistType, err := m3u8.DecodeFrom(playlistFile, true)
	if err != nil {
		return nil, err
	}

	// Close m3u8 file.
	err = playlistFile.Close()
	if err != nil {
		return nil, err
	}

	if !keepM3u8 {
		err = os.Remove(playlistPath)
		if err != nil {
			return nil, err
		}
	}

	if playlistType != m3u8.MEDIA {
		return nil, ErrWrongPlaylistType
	}

	// Cast and cache playlist.
	t.playlist = playlist.(*m3u8.MediaPlaylist)

	return t.playlist, nil
}

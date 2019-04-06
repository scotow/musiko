package musiko

import "github.com/grafov/m3u8"

type Part struct {
	data []byte
	seg  *m3u8.MediaSegment
}

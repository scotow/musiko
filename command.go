package musiko

import (
	"fmt"
	"github.com/google/uuid"
	"io"
	"os/exec"
	"path"
)

var (
	ffmpegCommand = "ffmpeg"
)

func commandExists(name string) bool {
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("command -v %s", name))
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func FfmpegInstalled() bool {
	return commandExists(ffmpegCommand)
}

func FfmpegSplitTS(reader io.Reader, dest string) (string, error) {
	id := uuid.New().String()

	playlist := path.Join(dest, fmt.Sprintf("%s.m3u8", id))
	ts := path.Join(dest, fmt.Sprintf("%s-%%d.ts", id))

	cmd := exec.Command(ffmpegCommand,
		"-i", "-",
		"-c", "copy",
		"-f", "segment",
		"-segment_list", playlist,
		"-segment_time", "10",
		"-segment_list_flags", "+live",
		ts)

	cmd.Stdin = reader

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return playlist, nil
}

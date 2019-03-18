package main

import (
	"fmt"
	"github.com/scotow/musiko"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

const (
	alternativeStationToken = "4156248623387312708"
)

var (
	stream       *musiko.Stream
	partsHandler http.Handler
)

func handle(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "/" {
		if shouldPlayer(r) {
			http.Redirect(w, r, "/player", http.StatusFound)
		} else {
			http.Redirect(w, r, "/playlist.m3u8", http.StatusFound)
		}
		return
	}

	if r.RequestURI == "/playlist.m3u8" {
		handlePlaylist(w, r)
		return
	}

	if strings.HasSuffix(r.RequestURI, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
		partsHandler.ServeHTTP(w, r)
		return
	}

	http.NotFound(w, r)
}

func shouldPlayer(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}

	if strings.Contains(accept, "text/html") {
		return true
	}

	return false
}

func handlePlaylist(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

	err := stream.WritePlaylist(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func main() {
	if !musiko.FfmpegInstalled() {
		log.Fatalln("ffmpeg not installed or cannot be found")
	}

	cred := musiko.Credentials{
		Username: os.Args[1],
		Password: os.Args[2],
	}

	partsDir, err := ioutil.TempDir("", "musiko")
	if err != nil {
		log.Fatalln(err)
	}

	s, err := musiko.NewStream(cred, partsDir, true)
	if err != nil {
		log.Fatalln(err)
	}
	stream = s

	fmt.Println(partsDir)
	partsHandler = http.FileServer(http.Dir(partsDir))

	http.Handle("/player/", http.StripPrefix("/player/", http.FileServer(http.Dir("player"))))
	http.HandleFunc("/", handle)

	_ = stream.Start(alternativeStationToken)

	log.Fatalln(http.ListenAndServe(":4889", nil))
}

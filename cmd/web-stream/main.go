package main

import (
	"flag"
	"github.com/scotow/musiko"
	"log"
	"net/http"
	"strconv"
	"strings"
)

const (
	alternativeStation = "4162959307923849796"
)

var (
	stream *musiko.Stream
)

// TODO: Use station name rather than ID.
var (
	usernameFlag = flag.String("u", "", "Pandora username (or e-mail address)")
	passwordFlag = flag.String("p", "", "Pandora password")
	stationFlag  = flag.String("s", alternativeStation, "Pandora station ID")
	portFlag     = flag.Int("P", 8080, "HTTP listening port")
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
		handlePart(w, r)
		return
	}

	http.NotFound(w, r)
}

func shouldPlayer(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func handlePlaylist(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

	err := stream.WritePlaylist(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func handlePart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp2t")

	var part string
	if strings.HasPrefix(r.RequestURI, "/") {
		part = r.RequestURI[1:]
	} else {
		part = r.RequestURI
	}

	err := stream.WritePart(w, part)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
}

func main() {
	flag.Parse()

	if !musiko.FfmpegInstalled() {
		log.Fatalln("ffmpeg not installed or cannot be found")
	}

	cred := musiko.Credentials{
		Username: *usernameFlag,
		Password: *passwordFlag,
	}

	s, err := musiko.NewStream(cred, *stationFlag, true)
	if err != nil {
		log.Fatalln("stream creation error:", err)
	}
	stream = s

	http.Handle("/player/", http.StripPrefix("/player/", http.FileServer(http.Dir("player"))))
	http.HandleFunc("/", handle)

	httpErr := make(chan error)

	streamErr, err := stream.Start()
	if err != nil {
		log.Fatalln("start stream error:", err)
	}

	// Start HTTP server.
	go func() {
		listeningAddress := ":" + strconv.Itoa(*portFlag)
		log.Println("Listening at", listeningAddress)

		err := http.ListenAndServe(listeningAddress, nil)
		httpErr <- err
	}()

	select {
	case err = <-streamErr:
	case err = <-httpErr:
		stream.Pause()
	}

	log.Fatalln(err)
}

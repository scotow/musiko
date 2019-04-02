package main

import (
	"flag"
	"fmt"
	"github.com/pkg/errors"
	"github.com/scotow/musiko"
	"github.com/scotow/musiko/timeout"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	alternativeStation = "G18"
	hipHopChillStation = "G1761"

	pauseTimeout = 90 * time.Second
	pauseTick    = 15 * time.Second
)

type radio struct {
	name   string
	stream *musiko.Stream
	pause  *timeout.AutoPauser
}

var (
	radios = make(map[string]*radio)
)

// TODO: Use station name rather than ID.
var (
	usernameFlag = flag.String("u", "", "Pandora username (or e-mail address)")
	passwordFlag = flag.String("p", "", "Pandora password")
	//stationFlag  = flag.String("s", alternativeStation, "Pandora station ID")
	portFlag = flag.Int("P", 8080, "HTTP listening port")
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

	if strings.HasSuffix(r.RequestURI, ".m3u8") {
		handlePlaylist(w, r)
		//autoPause.Reset()
		return
	}

	if strings.HasSuffix(r.RequestURI, ".ts") {
		handlePart(w, r)
		//autoPause.Reset()
		return
	}

	http.NotFound(w, r)
}

func shouldPlayer(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func handlePlaylist(w http.ResponseWriter, r *http.Request) {
	name := r.RequestURI[1 : len(r.RequestURI)-5]
	radio, e := radios[name]
	if !e {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

	err := radio.stream.WritePlaylist(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func handlePart(w http.ResponseWriter, r *http.Request) {
	/*var part string
	if strings.HasPrefix(r.RequestURI, "/") {
		part = r.RequestURI[1:]
	} else {
		part = r.RequestURI
	}*/

	split := strings.SplitN(r.RequestURI, ".", 2)
	if len(split) != 2 {
		http.NotFound(w, r)
		return
	}

	name := split[0][1:]
	radio, e := radios[name]
	if !e {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")

	err := radio.stream.WritePart(w, r.RequestURI[1:])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	radio.pause.Reset()
}

func createRadio(client *musiko.Client, station string, name string, report chan<- error) error {
	stationId, err := client.GetOrCreateStation(station)
	if err != nil {
		return errors.New(fmt.Sprint("station creation error:", err.Error()))
	}

	stream, err := musiko.NewStream(client, stationId, true)
	if err != nil {
		return errors.New(fmt.Sprint("stream creation error:", err.Error()))
	}

	stream.URIModifier = func(s string) string {
		return fmt.Sprintf("%s.%s", name, s)
	}

	err = stream.Start(report)
	if err != nil {
		return errors.New(fmt.Sprint("start stream error:", err.Error()))
	}

	pause := timeout.NewAutoPauser(stream, pauseTimeout, pauseTick)
	go func() {
		report <- pause.Start()
	}()

	radios[name] = &radio{name, stream, pause}
	return nil
}

func main() {
	flag.Parse()

	if !musiko.FfmpegInstalled() {
		log.Fatalln("ffmpeg not installed or cannot be found")
	}

	cred := musiko.Credentials{Username: *usernameFlag, Password: *passwordFlag}
	client, err := musiko.NewClient(cred)
	if err != nil {
		log.Fatalln("client creation error:", err)
	}

	report := make(chan error)

	err = createRadio(client, alternativeStation, "alternative", report)
	if err != nil {
		log.Fatalln(err)
	}
	err = createRadio(client, hipHopChillStation, "lo-fi", report)
	if err != nil {
		log.Fatalln(err)
	}

	http.Handle("/player/", http.StripPrefix("/player/", http.FileServer(http.Dir("player"))))
	http.HandleFunc("/", handle)

	// Start HTTP server.
	go func() {
		listeningAddress := ":" + strconv.Itoa(*portFlag)
		log.Println("Listening at", listeningAddress)

		err := http.ListenAndServe(listeningAddress, nil)
		report <- err
	}()

	err = <-report
	log.Fatalln(err)
}

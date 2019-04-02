package main

import (
	"encoding/json"
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

var (
	usernameFlag = flag.String("u", "", "Pandora username (or e-mail address)")
	passwordFlag = flag.String("p", "", "Pandora password")
	portFlag     = flag.Int("P", 8080, "HTTP listening port")

	stationsFlag configFlags
)

func handle(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "/" {
		if shouldPlayer(r) {
			http.Redirect(w, r, "/player", http.StatusFound)
		} else {
			http.Redirect(w, r, fmt.Sprintf("/%s.m3u8", stationsFlag[0].Name), http.StatusFound)
		}
		return
	}

	if strings.HasSuffix(r.RequestURI, ".m3u8") {
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

func handleStations(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(stationsFlag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
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

func createRadio(client *musiko.Client, stationId string, name string, report chan<- error) error {
	stationId, err := client.GetOrCreateStation(stationId)
	if err != nil {
		return errors.New(fmt.Sprint("station creation error:", err.Error()))
	}

	stream, err := musiko.NewStream(client, stationId, true)
	if err != nil {
		return errors.New(fmt.Sprint("stream creation error: ", err.Error()))
	}

	stream.URIModifier = func(s string) string {
		return fmt.Sprintf("%s.%s", name, s)
	}

	err = stream.Start(report)
	if err != nil {
		return errors.New(fmt.Sprint("start stream error: ", err.Error()))
	}

	pause := timeout.NewAutoPauser(stream, pauseTimeout, pauseTick)
	go func() {
		report <- pause.Start()
	}()

	radios[name] = &radio{name, stream, pause}
	return nil
}

func main() {
	if !musiko.FfmpegInstalled() {
		log.Fatalln("ffmpeg not installed or cannot be found")
	}

	flag.Var(&stationsFlag, "s", "Pandora stations with format \"display_name:genre_id\"")
	flag.Parse()

	if len(stationsFlag) < 1 {
		log.Fatalln("missing station configs")
	}

	cred := musiko.Credentials{Username: *usernameFlag, Password: *passwordFlag}
	client, err := musiko.NewClient(cred)
	if err != nil {
		log.Fatalln("client creation error:", err)
	}

	report := make(chan error)

	for _, station := range stationsFlag {
		err = createRadio(client, station.id, station.Name, report)
		if err != nil {
			log.Fatalln(err)
		}
	}

	http.Handle("/player/", http.StripPrefix("/player/", http.FileServer(http.Dir("player"))))
	http.HandleFunc("/stations", handleStations)
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

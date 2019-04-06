package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
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

func createRadio(client *musiko.Client, stationId string, name string, report chan<- error) error {
	stationId, err := client.GetOrCreateStation(stationId)
	if err != nil {
		return errors.New(fmt.Sprint("station creation error:", err.Error()))
	}

	stream, err := musiko.NewStream(client, stationId, true)
	if err != nil {
		return errors.New(fmt.Sprint("stream creation error: ", err.Error()))
	}

	stream.URIModifier = func(id string, in int) string {
		return fmt.Sprintf("/stations/%s/tracks/%s/parts/%d.ts", name, id, in)
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

func shouldPlayer(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func redirectToPlaylist(w http.ResponseWriter, r *http.Request, stationName string) {
	http.Redirect(w, r, fmt.Sprintf("/stations/%s/playlist.m3u8", stationName), http.StatusFound)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if shouldPlayer(r) {
		http.Redirect(w, r, "/player", http.StatusFound)
	} else {
		redirectToPlaylist(w, r, stationsFlag[0].Name)
	}
}

func playerHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "player/index.html")
}

func radioFromRequest(r *http.Request) *radio {
	vars := mux.Vars(r)

	stationName, exists := vars["name"]
	if !exists {
		return nil
	}

	radio, exists := radios[stationName]
	if !exists {
		return nil
	}

	return radio
}

func stationsListHandler(w http.ResponseWriter, _ *http.Request) {
	data, err := json.Marshal(stationsFlag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func redirectStationHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	stationName, exists := vars["name"]
	if !exists {
		http.NotFound(w, r)
		return
	}

	redirectToPlaylist(w, r, stationName)
}

func playlistHandler(w http.ResponseWriter, r *http.Request) {
	radio := radioFromRequest(r)
	if radio == nil {
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

func trackInfoHandler(w http.ResponseWriter, r *http.Request) {
	radio := radioFromRequest(r)
	if radio == nil {
		http.NotFound(w, r)
		return
	}

	trackId, exists := mux.Vars(r)["id"]
	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	err := radio.stream.WriteInfo(w, trackId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
}

func partHandler(w http.ResponseWriter, r *http.Request) {
	radio := radioFromRequest(r)
	if radio == nil {
		http.NotFound(w, r)
		return
	}

	vars := mux.Vars(r)

	trackId, exists := vars["id"]
	if !exists {
		http.NotFound(w, r)
		return
	}

	partIndex, exists := vars["index"]
	if !exists {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(partIndex, ".ts") {
		partIndex = partIndex[:len(partIndex)-3]
	}

	index, err := strconv.Atoi(partIndex)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")

	err = radio.stream.WritePartData(w, trackId, index)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	radio.pause.Reset()
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

	router := mux.NewRouter()

	// Stations, tracks and parts handlers.
	router.HandleFunc("/stations", stationsListHandler)
	router.HandleFunc("/stations/{name}", redirectStationHandler)
	router.HandleFunc("/stations/{name}/playlist.m3u8", playlistHandler)
	router.HandleFunc("/stations/{name}/tracks/{id}/info", trackInfoHandler)
	router.HandleFunc("/stations/{name}/tracks/{id}/parts/{index}", partHandler)

	// Player and root fallback handlers.
	router.PathPrefix("/player/").HandlerFunc(playerHandler)
	router.Handle("/player", http.RedirectHandler("/player/", http.StatusFound))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("player/static"))))
	router.HandleFunc("/", rootHandler)

	// Start HTTP server.
	go func() {
		listeningAddress := ":" + strconv.Itoa(*portFlag)
		log.Println("Listening at", listeningAddress)

		err := http.ListenAndServe(listeningAddress, router)
		report <- err
	}()

	err = <-report
	log.Fatalln(err)
}

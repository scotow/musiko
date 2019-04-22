package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/kennygrant/sanitize"
	"github.com/pkg/errors"
	"github.com/scotow/musiko"
	"github.com/scotow/musiko/timeout"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	radios         = make(map[string]*radio)
	defaultStation string
	lock           sync.Mutex
)

var (
	usernameFlag = flag.String("u", "", "Pandora username (or e-mail address)")
	passwordFlag = flag.String("p", "", "Pandora password")
	portFlag     = flag.Int("P", 8080, "HTTP listening port")
	defaultFlag  = flag.String("d", "", "default station")

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

	stream.URIModifier = func(trackId string, partIndex int) string {
		return fmt.Sprintf("/stations/%s/tracks/%s/parts/%d.ts", name, trackId, partIndex)
	}

	err = stream.Start(report)
	if err != nil {
		return errors.New(fmt.Sprint("start stream error: ", err.Error()))
	}

	pause := timeout.NewAutoPauser(stream, pauseTimeout, pauseTick)
	go func() {
		report <- pause.Start()
	}()

	lock.Lock()
	// Add the radio to the radio map.
	radios[name] = &radio{name, stream, pause}

	// Set the station as the default one if the name matches with the flag.
	if *defaultFlag != "" && *defaultFlag == name {
		defaultStation = name
	}
	lock.Unlock()

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
		redirectToPlaylist(w, r, defaultStation)
	}
}

func playerHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "player/index.html")
}

func radioFromRequest(r *http.Request) (*radio, bool) {
	vars := mux.Vars(r)

	stationName, exists := vars["name"]
	if !exists {
		return nil, false
	}

	radio, exists := radios[stationName]
	if !exists {
		return nil, false
	}

	return radio, true
}

func radioTrackFromRequest(r *http.Request) (*radio, string, bool) {
	radio, ok := radioFromRequest(r)
	if !ok {
		return nil, "", false
	}

	trackId, exists := mux.Vars(r)["id"]
	if !exists {
		return nil, "", false
	}

	return radio, trackId, true
}

func stationsListHandler(w http.ResponseWriter, _ *http.Request) {
	info := make(map[string]interface{})
	info["stations"] = stationsFlag
	info["default"] = defaultStation

	data, err := json.Marshal(info)
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
	radio, ok := radioFromRequest(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, err := radio.stream.WritePlaylist(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func trackInfoHandler(w http.ResponseWriter, r *http.Request) {
	radio, trackId, ok := radioTrackFromRequest(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err := radio.stream.WriteInfo(w, trackId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
}

func trackDownloadHandler(w http.ResponseWriter, r *http.Request) {
	radio, trackId, ok := radioTrackFromRequest(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	info, err := radio.stream.TrackInfo(trackId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.mp4", sanitize.BaseName(info.Name)))
	_, err = radio.stream.WriteTrack(w, trackId)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
}

func trackDownloadableHandler(w http.ResponseWriter, r *http.Request) {
	radio, trackId, ok := radioTrackFromRequest(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	data, err := json.Marshal(radio.stream.TrackAvailable(trackId))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func partHandler(w http.ResponseWriter, r *http.Request) {
	radio, trackId, ok := radioTrackFromRequest(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	partIndex, exists := mux.Vars(r)["index"]
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
	_, err = radio.stream.WritePartData(w, trackId, index)
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
	defaultStation = stationsFlag[0].Name

	var wg sync.WaitGroup
	wg.Add(len(stationsFlag))
	for _, s := range stationsFlag {
		go func(station config) {
			err = createRadio(client, station.id, station.Name, report)
			if err != nil {
				log.Fatalln(err)
			}
			wg.Done()
		}(s)
	}
	wg.Wait()

	log.Printf("Default station: %s.\n", defaultStation)

	router := mux.NewRouter()

	// Stations, tracks and parts handlers.
	router.HandleFunc("/stations", stationsListHandler)
	router.HandleFunc("/stations/{name}", redirectStationHandler)
	router.HandleFunc("/stations/{name}/playlist.m3u8", playlistHandler)
	router.HandleFunc("/stations/{name}/tracks/{id}/info", trackInfoHandler)
	router.HandleFunc("/stations/{name}/tracks/{id}/download", trackDownloadHandler)
	router.HandleFunc("/stations/{name}/tracks/{id}/downloadable", trackDownloadableHandler)
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

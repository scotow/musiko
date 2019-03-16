package main

import (
	"fmt"
	"github.com/scotow/musiko"
	"io"
	"log"
	"net/http"
	"os"
)

const (
	alternativeStationToken = "4156248623387312708"
)

var (
	client *musiko.Client
)

func handleMusic(w http.ResponseWriter, r *http.Request) {
	track, err := client.NextTrack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Println(track)

	resp, err := client.HttpClient.Get(track)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		log.Println("copy error", err)
	}
}

func main() {
	c, err := musiko.NewClient(os.Args[1], os.Args[2], alternativeStationToken)
	if err != nil {
		log.Fatalln(err)
	}
	client = c

	http.HandleFunc("/", handleMusic)
	log.Fatalln(http.ListenAndServe(":8080", nil))
}

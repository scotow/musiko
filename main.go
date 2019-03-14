package main

import (
	"fmt"
	"github.com/cellofellow/gopiano"
	"log"
	"os"
	"strings"
)

const (
	alternativeStationToken = "4156248623387312708"
)

var (
	qualitiesOrder = []string{"high", "medium", "low"}
)

func init() {
	for i, q := range qualitiesOrder {
		qualitiesOrder[i] = fmt.Sprintf("%sQuality", q)
	}
}

func higherQualityTracks(client *gopiano.Client, station string) ([]string, error) {
	resp, err := client.StationGetPlaylist(alternativeStationToken)
	if err != nil {
		return nil, err
	}

	tracks := make([]string, 0, len(resp.Result.Items))
	for _, item := range resp.Result.Items {
		for _, quality := range qualitiesOrder {
			if item, exists := item.AudioURLMap[quality]; exists {
				tracks = append(tracks, item.AudioURL)
				break
			}
		}
	}

	return tracks, nil
}

func main() {
	client, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = client.AuthPartnerLogin()
	if err != nil {
		log.Fatalln(err)
	}

	_, err = client.AuthUserLogin(os.Args[1], os.Args[2])
	if err != nil {
		log.Fatalln(err)
	}

	/*stations, err := client.UserGetStationList(false)
	if err != nil {
		log.Fatalln(err)
	}*/

	urls, err := higherQualityTracks(client, alternativeStationToken)
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(strings.Join(urls, "\n"))
}

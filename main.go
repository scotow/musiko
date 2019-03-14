package main

import (
	"fmt"
	"github.com/cellofellow/gopiano"
	"github.com/cellofellow/gopiano/responses"
	"log"
	"os"
)

const (
	alternativeStationToken = "4156248623387312708"
)

var (
	qualitiesOrder = []string{"high", "medium", "low"}
)

func higherQualityUrl(item responses.StationGetPlaylist) string {
	return ""
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

	resp, err := client.StationGetPlaylist(alternativeStationToken)
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(resp.Result.Items[0].AudioURLMap[qualitiesOrder[0]].AudioURL)
}

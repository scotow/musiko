package main

import (
	"flag"
	"fmt"
	"github.com/cellofellow/gopiano"
	"log"
)

const (
	alternativeStation = "G18"
)

var (
	usernameFlag = flag.String("u", "", "Pandora username (or e-mail address)")
	passwordFlag = flag.String("p", "", "Pandora password")
)

func main() {
	flag.Parse()

	client, err := gopiano.NewClient(gopiano.AndroidClient)
	if err != nil {
		log.Fatalln(err)
	}

	_, err = client.AuthPartnerLogin()
	if err != nil {
		log.Fatalln(err)
	}

	_, err = client.AuthUserLogin(*usernameFlag, *passwordFlag)
	if err != nil {
		log.Fatalln(err)
	}

	resp, err := client.StationCreateStationMusic(alternativeStation)
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(resp.Result.StationID)
}

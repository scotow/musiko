package main

import (
	"flag"
	"fmt"
	"github.com/cellofellow/gopiano"
	"log"
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

	resp, err := client.StationGetGenreStations()
	if err != nil {
		log.Fatalln(err)
	}

	for _, c := range resp.Result.Categories {
		for _, s := range c.Stations {
			fmt.Println(s.StationName, s.StationID)
		}
	}
}

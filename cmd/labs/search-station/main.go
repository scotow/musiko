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

	resp, err := client.MusicSearch("Lo-Fi")
	if err != nil {
		log.Fatalln(err)
	}

	for _, a := range resp.Result.Artists {
		fmt.Println(a.ArtistName, a.MusicToken)
	}
}

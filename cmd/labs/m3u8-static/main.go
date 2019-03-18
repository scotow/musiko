package main

import (
	"log"
	"net/http"
)

func main() {
	fs := http.FileServer(http.Dir("static-end-start"))
	http.Handle("/", fs)

	log.Fatalln(http.ListenAndServe(":8080", nil))
}

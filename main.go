package main

import (
	"net/http"

	log "github.com/Sirupsen/logrus"
	"github.com/alexrudd/am-rest-translator/translators"
)

func main() {
	for path, handle := range translators.Handles {
		http.HandleFunc(path, handle)
	}
	log.Fatal(http.ListenAndServe(":80", nil))
}

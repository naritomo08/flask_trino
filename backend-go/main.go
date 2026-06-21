package main

import (
	"log"
	"net/http"
	"time"
)

func main() {
	app, err := NewApp(NewTrinoClient(trinoURL))
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr:              ":5000",
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

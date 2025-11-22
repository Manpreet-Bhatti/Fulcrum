package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := flag.String("port", "5001", "Port to listen on")
	name := flag.String("name", "Server", "Name of the server")

	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] Received request from %s\n", *name, r.RemoteAddr)
		fmt.Fprintf(w, "Hello from Backend %s (Port %s)\n", *name, *port)
	})

	fmt.Printf("%s is listening on port %s...\n", *name, *port)

	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		fmt.Printf("Error starting server: %s\n", err)
		os.Exit(1)
	}
}

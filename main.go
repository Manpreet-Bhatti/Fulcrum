package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

type Config struct {
	LBPort   int      `json:"lb_port"`
	Backends []string `json:"backends"`
}

func LoadConfig(file string) (*Config, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var config Config
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&config)
	return &config, err
}

func main() {
	config, err := LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	fmt.Printf("⚖️  Fulcrum Load Balancer starting on port %d\n", config.LBPort)
	fmt.Printf("👉 Configured Backends: %v\n", config.Backends)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Fulcrum is alive! Request received at %s\n", r.URL.Path)
	})

	listenAddr := fmt.Sprintf(":%d", config.LBPort)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

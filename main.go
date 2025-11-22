package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync/atomic"
)

type Backend struct {
	URL          *url.URL
	ReverseProxy *httputil.ReverseProxy
}

type ServerPool struct {
	backends []Backend
	current  uint64
}

func (s *ServerPool) AddBackend(backend *Backend) {
	s.backends = append(s.backends, *backend)
}

// Return the next backend using Round Robin
func (s *ServerPool) GetNextPeer() *Backend {
	next := atomic.AddUint64(&s.current, 1)

	length := uint64(len(s.backends))
	idx := next % length

	return &s.backends[idx]
}

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

	serverPool := &ServerPool{}

	for _, u := range config.Backends {
		serverURL, err := url.Parse(u)
		if err != nil {
			log.Fatalf("Invalid backend URL: %v", err)
		}

		proxy := httputil.NewSingleHostReverseProxy(serverURL)
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = serverURL.Host
		}

		serverPool.AddBackend(&Backend{
			URL:          serverURL,
			ReverseProxy: proxy,
		})

		fmt.Printf("✅ Configured backend: %s\n", serverURL)
	}

	server := http.Server{
		Addr: fmt.Sprintf(":%d", config.LBPort),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := serverPool.GetNextPeer()

			if peer != nil {
				fmt.Printf("[Fulcrum] Dispatching to %s\n", peer.URL)
				peer.ReverseProxy.ServeHTTP(w, r)
			} else {
				http.Error(w, "Service not available", http.StatusServiceUnavailable)
			}
		}),
	}

	fmt.Printf("⚖️  Fulcrum Load Balancer starting on port %d\n", config.LBPort)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Backend struct {
	URL          *url.URL
	ReverseProxy *httputil.ReverseProxy
	Alive        bool
	mux          sync.RWMutex
}

func (backend *Backend) SetAlive(alive bool) {
	backend.mux.Lock()
	backend.Alive = alive
	backend.mux.Unlock()
}

func (backend *Backend) IsAlive() bool {
	backend.mux.RLock()
	defer backend.mux.RUnlock()
	return backend.Alive
}

type ServerPool struct {
	backends []*Backend ``
	current  uint64
}

func (serverPool *ServerPool) AddBackend(backend *Backend) {
	serverPool.backends = append(serverPool.backends, backend)
}

func (serverPool *ServerPool) nextIndex() int {
	return int(atomic.AddUint64(&serverPool.current, uint64(1)) % uint64(len(serverPool.backends)))
}

// Return the next ALIVE backend using Round Robin
func (serverPool *ServerPool) GetNextPeer() *Backend {
	next := serverPool.nextIndex()
	l := len(serverPool.backends) + next

	for i := next; i < l; i++ {
		idx := i % len(serverPool.backends)

		if serverPool.backends[idx].IsAlive() {
			if i != next {
				atomic.StoreUint64(&serverPool.current, uint64(idx))
			}

			return serverPool.backends[idx]
		}
	}

	return nil
}

func isBackendAlive(u *url.URL) bool {
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", u.Host, timeout)

	if err != nil {
		log.Println("Site unreachable, error: ", err)
		return false
	}

	_ = conn.Close()

	return true
}

func (serverPool *ServerPool) HealthCheck() {
	for _, backend := range serverPool.backends {
		status := "up"
		alive := isBackendAlive(backend.URL)
		backend.SetAlive(alive)

		if !alive {
			status = "down"
		}

		log.Printf("%s [%s]\n", backend.URL, status)
	}
}

func (serverPool *ServerPool) StartHealthCheck() {
	t := time.NewTicker(time.Second * 20)

	for range t.C {
		log.Println("Starting health check...")
		serverPool.HealthCheck()
		log.Println("Health check completed")
	}
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
		proxy.Director = func(req *http.Request) {
			req.Header.Add("X-Forwarded-Host", req.Host)
			req.Header.Add("X-Origin-Host", serverURL.Host)
			req.URL.Scheme = serverURL.Scheme
			req.URL.Host = serverURL.Host
		}

		serverPool.AddBackend(&Backend{
			URL:          serverURL,
			ReverseProxy: proxy,
			Alive:        true,
		})

		log.Printf("✅ Configured backend: %s\n", serverURL)
	}

	go serverPool.StartHealthCheck()

	server := http.Server{
		Addr: fmt.Sprintf(":%d", config.LBPort),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := serverPool.GetNextPeer()

			if peer != nil {
				peer.ReverseProxy.ServeHTTP(w, r)
				return
			}

			http.Error(w, "Service not available", http.StatusServiceUnavailable)
		}),
	}

	log.Printf("⚖️  Fulcrum Load Balancer starting on port %d\n", config.LBPort)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"context"
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

type contextKey string

const RetryAttempts int = 3
const RetryCtxKey contextKey = "retry"

type Backend struct {
	URL               *url.URL
	ReverseProxy      *httputil.ReverseProxy
	Alive             bool
	mux               sync.RWMutex
	ActiveConnections int64
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

// Returns the next ALIVE backend using Round Robin
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

// Returns the server with the least number of active connections
func (serverPool *ServerPool) GetNextPeerLeastConnections() *Backend {
	var bestPeer *Backend = nil
	var minConns int64 = -1

	for _, backend := range serverPool.backends {
		if !backend.IsAlive() {
			continue
		}

		conn := atomic.LoadInt64(&backend.ActiveConnections)

		if bestPeer == nil || conn < minConns {
			bestPeer = backend
			minConns = conn
		}
	}

	return bestPeer
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

func (serverPool *ServerPool) MarkBackendStatus(u *url.URL, alive bool) {
	for _, backend := range serverPool.backends {
		if backend.URL.String() == u.String() {
			backend.SetAlive(alive)
			break
		}
	}
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

type WrappedWriter struct {
	http.ResponseWriter
	StatusCode int
}

// Capture status code before writing it
func (w *WrappedWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	w.StatusCode = statusCode
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Spy on status code
		wrapped := &WrappedWriter{
			ResponseWriter: w,
			StatusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		log.Printf("REQ: %s %s | STATUS: %d | TIME: %v", r.Method, r.URL.Path, wrapped.StatusCode, duration)
	})
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
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
			log.Printf("[%s] %s", serverURL.Host, e.Error())

			serverPool.MarkBackendStatus(serverURL, false)

			retries, _ := r.Context().Value(RetryCtxKey).(int)

			if retries < RetryAttempts {
				retryPeer := serverPool.GetNextPeer()

				if retryPeer != nil {
					log.Printf("[Fulcrum] Retrying request on %s (Attempt %d)", retryPeer.URL, retries+1)

					ctx := context.WithValue(r.Context(), RetryCtxKey, retries+1)

					retryPeer.ReverseProxy.ServeHTTP(w, r.WithContext(ctx))

					return
				}
			}

			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("[Fulcrum] All backends failed"))
		}

		serverPool.AddBackend(&Backend{
			URL:          serverURL,
			ReverseProxy: proxy,
			Alive:        true,
		})
	}

	go serverPool.StartHealthCheck()

	lbHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), RetryCtxKey, 0)
		peer := serverPool.GetNextPeerLeastConnections()

		if peer != nil {
			atomic.AddInt64(&peer.ActiveConnections, 1)
			defer atomic.AddInt64(&peer.ActiveConnections, -1)
			peer.ReverseProxy.ServeHTTP(w, r.WithContext(ctx))

			return
		}

		http.Error(w, "Service not available", http.StatusServiceUnavailable)
	})

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", config.LBPort),
		Handler: LoggingMiddleware(lbHandler),
	}

	log.Printf("⚖️  Fulcrum Load Balancer starting on port %d\n", config.LBPort)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

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
	Name              string
	URL               *url.URL
	ReverseProxy      *httputil.ReverseProxy
	Alive             bool
	mux               sync.RWMutex
	ActiveConnections int64
	TotalRequests     uint64
	FailedRequests    uint64
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

type BackendConfig struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type Config struct {
	LBPort   int             `json:"lb_port"`
	Backends []BackendConfig `json:"backends"`
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

func (serverPool *ServerPool) GetBackend(u *url.URL) *Backend {
	for _, b := range serverPool.backends {
		if b.URL.String() == u.String() {
			return b
		}
	}

	return nil
}

func (s *ServerPool) ServeDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.backends)

		return
	}

	w.Header().Set("Content-Type", "text/html")

	html := `<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<title>Fulcrum Command Center</title>
		<style>
			:root { --bg: #0f172a; --card: #1e293b; --text: #e2e8f0; --green: #22c55e; --red: #ef4444; --yellow: #eab308; }
			body { font-family: 'Courier New', Courier, monospace; background: var(--bg); color: var(--text); padding: 20px; margin: 0; }
			h1 { border-bottom: 2px solid var(--text); padding-bottom: 10px; margin-bottom: 30px; letter-spacing: -1px; }
			
			/* Grid Layout */
			.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
			
			/* Card Styling */
			.card { background: var(--card); border: 1px solid #334155; padding: 20px; border-radius: 8px; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1); transition: transform 0.2s; }
			.card:hover { transform: translateY(-2px); border-color: #475569; }
			
			/* Header inside card */
			.header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 15px; border-bottom: 1px solid #334155; padding-bottom: 10px; }
			.url { font-weight: bold; font-size: 1.1em; color: #60a5fa; }
			
			/* Metrics */
			.stats { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; font-size: 0.9em; }
			.stat-item { display: flex; flex-direction: column; }
			.label { color: #94a3b8; font-size: 0.8em; text-transform: uppercase; letter-spacing: 0.5px; }
			.value { font-size: 1.2em; font-weight: bold; }
			
			/* Status Badges */
			.badge { padding: 4px 8px; border-radius: 4px; font-size: 0.8em; font-weight: bold; text-transform: uppercase; }
			.up { background: rgba(34, 197, 94, 0.1); color: var(--green); border: 1px solid var(--green); }
			.down { background: rgba(239, 68, 68, 0.1); color: var(--red); border: 1px solid var(--red); }
			
			/* Footer */
			.footer { margin-top: 30px; font-size: 0.8em; color: #64748b; text-align: center; }
			a { color: #60a5fa; text-decoration: none; }
		</style>
	</head>
	<body>
		<h1>⚡ FULCRUM <span style="font-size:0.5em; color:#64748b;">// LOAD BALANCER CLI</span></h1>
		
		<div class="grid">`

	for _, backend := range s.backends {
		alive := backend.IsAlive()
		statusBadge := "<span class='badge up'>ONLINE</span>"

		if !alive {
			statusBadge = "<span class='badge down'>OFFLINE</span>"
		}

		active := atomic.LoadInt64(&backend.ActiveConnections)
		total := atomic.LoadUint64(&backend.TotalRequests)
		failed := atomic.LoadUint64(&backend.FailedRequests)
		errorRate := 0.0

		if total > 0 {
			errorRate = (float64(failed) / float64(total)) * 100
		}

		html += fmt.Sprintf(`
			<div class="card">
				<div class="header">
					<span class="url">%s</span>
					%s
				</div>
				<div style="font-size: 0.8em; color: #94a3b8; margin-bottom: 15px;">%s</div>
				<div class="stats">
					<div class="stat-item">
						<span class="label">Active Conns</span>
						<span class="value" style="color: #eab308;">%d</span>
					</div>
					<div class="stat-item">
						<span class="label">Total Reqs</span>
						<span class="value">%d</span>
					</div>
					<div class="stat-item">
						<span class="label">Failures</span>
						<span class="value" style="color: #ef4444;">%d</span>
					</div>
					<div class="stat-item">
						<span class="label">Error Rate</span>
						<span class="value">%.2f%%</span>
					</div>
				</div>
			</div>
		`, backend.Name, statusBadge, backend.URL, active, total, failed, errorRate)
	}

	html += `</div>
		<div class="footer">
			System Status: OPERATIONAL | <a href="?format=json">View Raw JSON</a>
		</div>
		<script>
			// Auto-refresh logic that preserves scroll position
			setTimeout(() => {
				window.location.reload();
			}, 2000);
		</script>
	</body>
	</html>`

	fmt.Fprint(w, html)
}

func main() {
	config, err := LoadConfig("config.json")

	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	serverPool := &ServerPool{}

	for _, u := range config.Backends {
		serverURL, err := url.Parse(u.URL)

		if err != nil {
			log.Fatalf("Invalid backend URL: %v", err)
		}

		proxy := httputil.NewSingleHostReverseProxy(serverURL)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
			log.Printf("[%s] %s", serverURL.Host, e.Error())

			if b := serverPool.GetBackend(serverURL); b != nil {
				atomic.AddUint64(&b.FailedRequests, 1)
			}

			serverPool.MarkBackendStatus(serverURL, false)

			retries, _ := r.Context().Value(RetryCtxKey).(int)

			if retries < RetryAttempts {
				time.Sleep(10 * time.Millisecond)

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
			Name:         u.Name,
			URL:          serverURL,
			ReverseProxy: proxy,
			Alive:        true,
		})
	}

	go serverPool.StartHealthCheck()

	go func() {
		log.Println("📊 Dashboard started at :8081")
		http.ListenAndServe(":8081", http.HandlerFunc(serverPool.ServeDashboard))
	}()

	lbHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), RetryCtxKey, 0)
		peer := serverPool.GetNextPeerLeastConnections()

		if peer != nil {
			atomic.AddInt64(&peer.ActiveConnections, 1)
			atomic.AddUint64(&peer.TotalRequests, 1)
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

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Manpreet-Bhatti/Fulcrum/config"
	"github.com/Manpreet-Bhatti/Fulcrum/limiter"
	"github.com/Manpreet-Bhatti/Fulcrum/middleware"
	"github.com/Manpreet-Bhatti/Fulcrum/pool"
)

func main() {
	cfg, err := config.LoadConfig("config.json")

	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	serverPool := &pool.ServerPool{}

	for _, u := range cfg.Backends {
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

			retries, _ := r.Context().Value(pool.RetryCtxKey).(int)

			if retries < pool.RetryAttempts {
				time.Sleep(10 * time.Millisecond)

				retryPeer := serverPool.GetNextPeer()

				if retryPeer != nil {
					log.Printf("[Fulcrum] Retrying request on %s (Attempt %d)", retryPeer.URL, retries+1)

					ctx := context.WithValue(r.Context(), pool.RetryCtxKey, retries+1)

					retryPeer.ReverseProxy.ServeHTTP(w, r.WithContext(ctx))

					return
				}
			}

			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("[Fulcrum] All backends failed"))
		}

		proxy.ModifyResponse = func(response *http.Response) error {
			if response.StatusCode >= 500 {
				backend := serverPool.GetBackend(serverURL)

				if backend != nil {
					failures := atomic.AddUint64(&backend.ConsecutiveFailures, 1)

					if failures >= 3 {
						log.Printf("🔥 CIRCUIT BREAKER TRIGGERED: %s marked down", serverURL)

						backend.SetAlive(false)
					}
				}
			} else {
				backend := serverPool.GetBackend(serverURL)

				if backend != nil {
					atomic.StoreUint64(&backend.ConsecutiveFailures, 0)
				}
			}

			return nil
		}

		weight := u.Weight
		if weight == 0 {
			weight = 1
		}

		backend := &pool.Backend{
			Name:         u.Name,
			URL:          serverURL,
			ReverseProxy: proxy,
			Alive:        true,
		}

		for i := 0; i < weight; i++ {
			serverPool.AddBackend(backend)
		}

		log.Printf("Configured backend: %s (Weight: %d)\n", serverURL, weight)
	}

	go serverPool.StartHealthCheck()

	go func() {
		log.Println("📊 Dashboard started at :8081")
		http.ListenAndServe(":8081", http.HandlerFunc(serverPool.ServeDashboard))
	}()

	rateLimiter := limiter.NewIPRateLimiter(10, 20)

	lbHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)

		if err != nil {
			ip = r.RemoteAddr
		}

		limiter := rateLimiter.GetLimiter(ip)

		if !limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		ctx := context.WithValue(r.Context(), pool.RetryCtxKey, 0)

		peer := serverPool.GetNextPeer()

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
		Addr:    fmt.Sprintf(":%d", cfg.LBPort),
		Handler: middleware.LoggingMiddleware(lbHandler),
	}

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		log.Printf("🔒 Secure Fulcrum LB started at :%d (HTTPS)\n", cfg.LBPort)

		if err := server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil {
			log.Fatal(err)
		}
	} else {
		log.Printf("⚖️  Fulcrum Load Balancer started at :%d (HTTP)\n", cfg.LBPort)

		if err := server.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}
}

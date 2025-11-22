package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/Manpreet-Bhatti/Fulcrum/config"
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

		serverPool.AddBackend(&pool.Backend{
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
		ctx := context.WithValue(r.Context(), pool.RetryCtxKey, 0)
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
		Addr:    fmt.Sprintf(":%d", cfg.LBPort),
		Handler: middleware.LoggingMiddleware(lbHandler),
	}

	log.Printf("⚖️  Fulcrum Load Balancer starting on port %d\n", cfg.LBPort)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

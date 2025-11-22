package pool

import (
	"net/http/httputil"
	"net/url"
	"sync"
)

type contextKey string

const RetryAttempts int = 3
const RetryCtxKey contextKey = "retry"

type Backend struct {
	Name              string
	URL               *url.URL
	ReverseProxy      *httputil.ReverseProxy `json:"-"`
	Alive             bool
	Mux               sync.RWMutex `json:"-"`
	ActiveConnections int64
	TotalRequests     uint64
	FailedRequests    uint64
}

func (backend *Backend) SetAlive(alive bool) {
	backend.Mux.Lock()
	backend.Alive = alive
	backend.Mux.Unlock()
}

func (backend *Backend) IsAlive() bool {
	backend.Mux.RLock()
	defer backend.Mux.RUnlock()
	return backend.Alive
}

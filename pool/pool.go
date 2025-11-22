package pool

import (
	"net/url"
	"sync/atomic"
)

type ServerPool struct {
	Backends []*Backend
	current  uint64
}

func (serverPool *ServerPool) AddBackend(backend *Backend) {
	serverPool.Backends = append(serverPool.Backends, backend)
}

func (serverPool *ServerPool) nextIndex() int {
	return int(atomic.AddUint64(&serverPool.current, uint64(1)) % uint64(len(serverPool.Backends)))
}

// Returns the next ALIVE backend using Round Robin
func (serverPool *ServerPool) GetNextPeer() *Backend {
	next := serverPool.nextIndex()
	l := len(serverPool.Backends) + next

	for i := next; i < l; i++ {
		idx := i % len(serverPool.Backends)

		if serverPool.Backends[idx].IsAlive() {
			if i != next {
				atomic.StoreUint64(&serverPool.current, uint64(idx))
			}

			return serverPool.Backends[idx]
		}
	}

	return nil
}

// Returns the server with the least number of active connections
func (serverPool *ServerPool) GetNextPeerLeastConnections() *Backend {
	var bestPeer *Backend = nil
	var minConns int64 = -1

	for _, backend := range serverPool.Backends {
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

func (serverPool *ServerPool) MarkBackendStatus(u *url.URL, alive bool) {
	for _, backend := range serverPool.Backends {
		if backend.URL.String() == u.String() {
			backend.SetAlive(alive)
			break
		}
	}
}

func (serverPool *ServerPool) GetBackend(u *url.URL) *Backend {
	for _, b := range serverPool.Backends {
		if b.URL.String() == u.String() {
			return b
		}
	}

	return nil
}

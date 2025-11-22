package pool

import (
	"log"
	"net"
	"net/url"
	"time"
)

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
	for _, backend := range serverPool.Backends {
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

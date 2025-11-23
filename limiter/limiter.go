package limiter

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	ips      map[string]*rate.Limiter
	lastSeen map[string]time.Time
	mu       sync.RWMutex
	r        rate.Limit
	b        int
}

func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	i := &IPRateLimiter{
		ips:      make(map[string]*rate.Limiter),
		lastSeen: make(map[string]time.Time),
		r:        r,
		b:        b,
	}

	go i.CleanupLoop()

	return i
}

func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()

	defer i.mu.Unlock()

	limiter, exists := i.ips[ip]

	if !exists {
		limiter = rate.NewLimiter(i.r, i.b)
		i.ips[ip] = limiter
	}

	i.lastSeen[ip] = time.Now()

	return limiter
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()

	limiter, exists := i.ips[ip]

	if !exists {
		i.mu.Unlock()

		return i.AddIP(ip)
	}

	i.lastSeen[ip] = time.Now()
	i.mu.Unlock()

	return limiter
}

func (i *IPRateLimiter) CleanupLoop() {
	for {
		time.Sleep(1 * time.Minute)

		i.mu.Lock()

		for ip, t := range i.lastSeen {
			if time.Since(t) > 3*time.Minute {
				delete(i.ips, ip)
				delete(i.lastSeen, ip)
			}
		}

		i.mu.Unlock()
	}
}

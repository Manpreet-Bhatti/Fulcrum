package middleware

import (
	"log"
	"net/http"
	"time"
)

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

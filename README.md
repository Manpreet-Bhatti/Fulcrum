# ⚖️ Fulcrum

Fulcrum is a reverse proxy and load balancer capable of distributing HTTP traffic across a cluster of backend servers. Built with a focus on concurrency control, retries (fault tolerance), and observability, it features active health checks, automatic failover retries, and a real-time analytics dashboard.

## 🚀 Key Features

- **Intelligent Routing:**
  - **Weighted Round Robin:** Assigns traffic loads based on server capacity (weights).
  - **Least Connections:** Dynamically routes traffic to the least busy server.
- **Hybrid Health Checks:**
  - **Active:** Periodically pings backends via TCP to check connectivity.
  - **Passive (Circuit Breaker):** Instantly detects 5xx error spikes and temporarily removes unstable nodes from rotation.
- **Resilience & Security:**
  - **TLS Termination:** Offloads SSL/TLS encryption processing from backend servers, managing certificates at the entry point.
  - **Rate Limiting:** Implements a **Token Bucket** algorithm (per IP) to prevent DDoS attacks and "noisy neighbor" resource exhaustion.
  - **Automatic Retries:** Seamlessly fails over to healthy backends if a connection is refused, transparent to the user.
- **Live Dashboard:** A visualization of connection pools, error rates, and server status in real-time.
- **Observability:** Custom middleware for detailed request logging (latency, status codes, method).
- **Concurrency Safe:** Uses `sync/atomic` for lock-free counter increments and `sync.RWMutex` for safe state management.

## 🛠️ Architecture

Fulcrum acts as a Layer 7 Proxy. It accepts incoming traffic on a defined port, modifies the headers (X-Forwarded-For), and streams the request to a healthy backend.

```
User Request  --->  [ FULCRUM LB (:8443) ]  --->  [ Backend A (:5001) ]
                           |                    |
                           |                    --->  [ Backend B (:5002) ]
                    [ Health Checker ]          |
                           |                    --->  [ Backend C (:5003) ]
                    [ Dashboard (:8081) ]
```

## 📦 Getting Started

**Prerequisites**

- Go 1.21+
- OpenSSL (optional, for generating self-signed certs)

1. **Clone the repo**

```bash
git clone https://github.com/Manpreet-Bhatti/Fulcrum.git
cd Fulcrum
go mod tidy
```

2. **Generate SSL Keys (Optional)**

To enable HTTPS (TLS Termination), generate self-signed keys in the root folder:

```bash
openssl req -new -newkey rsa:2048 -days 365 -nodes -x509 \
  -keyout server.key -out server.crt \
  -subj "/C=<country>/ST=<province/state>/L=<city>/O=<organization>/CN=localhost"
```

3. **Configure Backends**

```json
{
  "lb_port": 8443,
  "tls_cert": "server.crt",
  "tls_key": "server.key",
  "backends": [
    {
      "name": "backend-1",
      "url": "http://localhost:5001",
      "weight": 3
    },
    {
      "name": "backend-2",
      "url": "http://localhost:5002",
      "weight": 1
    },
    {
      "name": "backend-3",
      "url": "http://localhost:5003",
      "weight": 1
    }
  ]
}
```

4. **Start the Dummy Backend Cluster**

This repo includes a helper tool to spin up test servers. Open 3 terminal tabs:

```bash
# Terminal 1
go run backend/main.go -port 5001 -name "backend-1"

# Terminal 2
go run backend/main.go -port 5002 -name "backend-2"

# Terminal 3
go run backend/main.go -port 5003 -name "backend-3"
```

5. **Run Fulcrum**

```bash
go run main.go
```

## 🎮 Usage & Demo

### Send Secure Traffic

Since we are using a self-signed certificate, use curl -k (insecure mode) to verify the connection:

```bash
curl -k -v https://localhost:8443
```

*Observe the TLS handshake in the curl output, and the plain HTTP logs in the backend terminals.*

### View Dashboard

Open your browser to: http://localhost:8081

- Watch **Active Requests** spike during load.
- Observe **Failures** and **Error Rates** calculated in real-time.

### Test Rate Limiting

Spam the server with requests:

```bash
for i in {1..25}; do curl -k -s -o /dev/null -w "%{http_code}\n" https://localhost:8443; done
```

*Result: You will see `200` responses followed by a stream of `429` errors once the token bucket is empty.*

### Test Circuit Breaker

1. Force a backend to return 500 errors (or kill the process).
2. Fulcrum will detect the consecutive failures and mark the node **OFFLINE** immediately, bypassing the 20s health check interval.

### Test Weighted Routing

1. Send a burst of requests (e.g., using `curl`).
2. Observe that `backend-1` (Weight 3) receives approximately 3x more traffic than the backup nodes.

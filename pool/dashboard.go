package pool

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
)

func (s *ServerPool) ServeDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Backends)

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

	for _, backend := range s.Backends {
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

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

type Server struct {
	port int
}

func NewServer(port int) *Server {
	return &Server{port: port}
}

func (s *Server) Start() error {
	http.HandleFunc("/", s.handleDashboard)
	http.HandleFunc("/api/stats", s.handleStats)
	http.HandleFunc("/api/jobs", s.handleJobs)
	http.HandleFunc("/api/executions", s.handleExecutions)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Dashboard server starting on http://localhost%s", addr)
	return http.ListenAndServe(addr, nil)
}
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := GetExecutionStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)

}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	counts, err := GetJobCountsByState()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make(map[string]int)
	for state, count := range counts {
		result[string(state)] = count
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleExecutions(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	executions, err := GetRecentExecutions(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(executions)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	tmpl := `<!DOCTYPE html>
<html>
<head>
	<title>QueueCTL Dashboard</title>
	<style>
	body {
		font-family: 'Segoe UI', Roboto, sans-serif;
		margin: 0;
		padding: 20px;
		background-color: #0d1117;
		color: #e6edf3;
	}

	.container {
		max-width: 1200px;
		margin: 0 auto;
		background: #161b22;
		padding: 30px;
		border-radius: 10px;
		box-shadow: 0 0 20px rgba(0, 0, 0, 0.5);
	}

	h1 {
		color: #58a6ff;
		border-bottom: 2px solid #30363d;
		padding-bottom: 10px;
		margin-bottom: 20px;
		font-size: 28px;
		letter-spacing: 0.5px;
		text-shadow: 0 0 6px rgba(88, 166, 255, 0.4);
	}

	h2 {
		color: #58a6ff;
		margin-top: 40px;
		font-size: 20px;
		text-shadow: 0 0 6px rgba(88, 166, 255, 0.4);
	}

	.stats-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 20px;
		margin: 30px 0;
	}

	.stat-card {
		background: #21262d;
		padding: 15px 20px;
		border-radius: 8px;
		border: 1px solid #30363d;
		transition: transform 0.2s ease, box-shadow 0.2s ease;
	}

	.stat-card:hover {
		transform: translateY(-3px);
		box-shadow: 0 0 10px rgba(88, 166, 255, 0.3);
	}

	.stat-label {
		font-size: 12px;
		color: #8b949e;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.stat-value {
		font-size: 26px;
		font-weight: bold;
		color: #e6edf3;
		margin-top: 8px;
		transition: opacity 0.3s ease;
	}

	table {
		width: 100%;
		border-collapse: collapse;
		margin-top: 15px;
		border: 1px solid #30363d;
		border-radius: 6px;
		overflow: hidden;
	}

	th, td {
		padding: 12px 10px;
		text-align: left;
		border-bottom: 1px solid #30363d;
	}

	th {
		background-color: #21262d;
		color: #58a6ff;
		text-transform: uppercase;
		font-size: 13px;
		letter-spacing: 0.03em;
	}

	tr:hover {
		background-color: #1f6feb22;
	}

	.status-pending { color: #f39c12; font-weight: bold; }
	.status-processing { color: #1f6feb; font-weight: bold; }
	.status-completed { color: #2ecc71; font-weight: bold; }
	.status-failed { color: #e74c3c; font-weight: bold; }
	.status-dead { color: #95a5a6; font-weight: bold; }

	.success { color: #2ecc71; }
	.failure { color: #e74c3c; }
	.timeout { color: #f39c12; }

	.refresh-info {
		text-align: right;
		color: #8b949e;
		font-size: 12px;
		margin-top: 15px;
	}
	</style>
</head>
<body>
	<div class="container">
		<h1>QueueCTL Dashboard</h1>
		
		<div class="stats-grid">
			<div class="stat-card"><div class="stat-label">Total Processed</div><div class="stat-value" id="total-processed">-</div></div>
			<div class="stat-card"><div class="stat-label">Succeeded</div><div class="stat-value success" id="total-succeeded">-</div></div>
			<div class="stat-card"><div class="stat-label">Failed</div><div class="stat-value failure" id="total-failed">-</div></div>
			<div class="stat-card"><div class="stat-label">Timeouts</div><div class="stat-value timeout" id="total-timeout">-</div></div>
			<div class="stat-card"><div class="stat-label">Success Rate</div><div class="stat-value" id="success-rate">-</div></div>
			<div class="stat-card"><div class="stat-label">Avg Duration</div><div class="stat-value" id="avg-duration">-</div></div>
		</div>

		<h2>Queue Status</h2>
		<table id="queue-status">
			<thead><tr><th>State</th><th>Count</th></tr></thead>
			<tbody id="queue-status-body"></tbody>
		</table>

		<h2>Recent Executions</h2>
		<table id="executions">
			<thead><tr><th>Job ID</th><th>Command</th><th>Started</th><th>Duration</th><th>Status</th></tr></thead>
			<tbody id="executions-body"></tbody>
		</table>

		<div class="refresh-info">Auto-updating every 5 seconds (without full reload)</div>
	</div>

	<script>
		function fadeUpdate(element, newValue) {
			if (element.textContent !== newValue) {
				element.style.opacity = 0.3;
				setTimeout(() => {
					element.textContent = newValue;
					element.style.opacity = 1;
				}, 200);
			}
		}

		function updateStats() {
			fetch('/api/stats')
				.then(r => r.json())
				.then(data => {
					fadeUpdate(document.getElementById('total-processed'), data.total_processed || 0);
					fadeUpdate(document.getElementById('total-succeeded'), data.total_succeeded || 0);
					fadeUpdate(document.getElementById('total-failed'), data.total_failed || 0);
					fadeUpdate(document.getElementById('total-timeout'), data.total_timeout || 0);
					fadeUpdate(document.getElementById('success-rate'), (data.success_rate || 0).toFixed(1) + '%');
					fadeUpdate(document.getElementById('avg-duration'), (data.avg_duration_ms || 0).toFixed(0) + 'ms');
				});
		}

		function updateQueueStatus() {
			fetch('/api/jobs')
				.then(r => r.json())
				.then(data => {
					const tbody = document.getElementById('queue-status-body');
					tbody.innerHTML = '';
					const states = ['pending', 'processing', 'completed', 'failed', 'dead'];
					states.forEach(state => {
						const count = data[state] || 0;
						const row = document.createElement('tr');
						row.innerHTML = '<td class="status-' + state + '">' + state + '</td><td>' + count + '</td>';
						tbody.appendChild(row);
					});
				});
		}

		function updateExecutions() {
			fetch('/api/executions')
				.then(r => r.json())
				.then(data => {
					const tbody = document.getElementById('executions-body');
					tbody.innerHTML = '';
					data.forEach(exec => {
						const row = document.createElement('tr');
						const status = exec.success ? 
							'<span class="success">Success</span>' : 
							(exec.timeout ? '<span class="timeout">Timeout</span>' : '<span class="failure">Failed</span>');
						const duration = exec.duration_ms ? exec.duration_ms + 'ms' : '-';
						const started = exec.started_at ? new Date(exec.started_at).toLocaleString() : '-';
						row.innerHTML = '<td>' + exec.job_id +'</td><td>'+ exec.command+ '</td><td>' + started + '</td><td>' + duration + '</td><td>' + status + '</td>';
						tbody.appendChild(row);
					});
				});
		}

		function updateAll() {
			updateStats();
			updateQueueStatus();
			updateExecutions();
		}

		updateAll();
		setInterval(updateAll, 5000);
	</script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, tmpl)
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LogBuffer struct {
	mu       sync.RWMutex
	lines    []string
	partial  string
	maxLines int
}

func NewLogBuffer(maxLines int) *LogBuffer {
	return &LogBuffer{maxLines: maxLines}
}

func (buffer *LogBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	text := buffer.partial + string(data)
	parts := strings.Split(text, "\n")
	buffer.partial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		buffer.lines = append(buffer.lines, line)
	}
	if len(buffer.lines) > buffer.maxLines {
		buffer.lines = buffer.lines[len(buffer.lines)-buffer.maxLines:]
	}
	return len(data), nil
}

func (buffer *LogBuffer) Lines() []string {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	lines := make([]string, len(buffer.lines))
	copy(lines, buffer.lines)
	return lines
}

type DashboardServer struct {
	address string
	store   *StatsStore
	logs    *LogBuffer
	logger  io.Writer
	server  *http.Server
}

func NewDashboardServer(address string, store *StatsStore, logs *LogBuffer, logger io.Writer) *DashboardServer {
	dashboard := &DashboardServer{
		address: address,
		store:   store,
		logs:    logs,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", dashboard.handleIndex)
	mux.HandleFunc("GET /api/stats", dashboard.handleStats)
	mux.HandleFunc("GET /api/recent", dashboard.handleRecent)
	mux.HandleFunc("GET /api/logs", dashboard.handleLogs)
	mux.HandleFunc("GET /health", dashboard.handleHealth)
	dashboard.server = &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return dashboard
}

func (dashboard *DashboardServer) Start() {
	go func() {
		fmt.Fprintf(dashboard.logger, "Dashboard listening on %s\n", dashboard.address)
		if err := dashboard.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(dashboard.logger, "Dashboard error: %v\n", err)
		}
	}()
}

func (dashboard *DashboardServer) Close() error {
	return dashboard.server.Close()
}

func (dashboard *DashboardServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, dashboardHTML)
}

func (dashboard *DashboardServer) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats, err := dashboard.store.Summary()
	dashboard.writeJSON(w, stats, err)
}

func (dashboard *DashboardServer) handleRecent(w http.ResponseWriter, _ *http.Request) {
	assets, err := dashboard.store.Recent(50)
	dashboard.writeJSON(w, assets, err)
}

func (dashboard *DashboardServer) handleLogs(w http.ResponseWriter, _ *http.Request) {
	dashboard.writeJSON(w, dashboard.logs.Lines(), nil)
}

func (dashboard *DashboardServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

func (dashboard *DashboardServer) writeJSON(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Immich Optimizer</title>
<style>
:root{color-scheme:dark;--bg:#0b1020;--panel:#141b2d;--line:#28324a;--text:#eef2ff;--muted:#94a3b8;--accent:#818cf8}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px system-ui,sans-serif}
main{max-width:1600px;margin:auto;padding:24px}h1{margin:0 0 20px;font-size:24px}.cards{display:grid;grid-template-columns:repeat(5,1fr);gap:14px}
.card,.panel{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:18px}.label{color:var(--muted);font-size:12px;text-transform:uppercase}.value{font-size:28px;font-weight:700;margin-top:8px}
.grid{display:grid;grid-template-columns:1.2fr 1fr;gap:14px;margin-top:14px}.panel{overflow:hidden}.panel h2{font-size:15px;margin:0 0 14px}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:9px;border-bottom:1px solid var(--line)}th{color:var(--muted);font-size:11px;text-transform:uppercase}
#logs{height:440px;overflow:auto;white-space:pre-wrap;font:12px ui-monospace,monospace;color:#cbd5e1}.good{color:#86efac}
@media(max-width:1100px){.cards{grid-template-columns:repeat(3,1fr)}.grid{grid-template-columns:1fr}}@media(max-width:700px){.cards{grid-template-columns:repeat(2,1fr)}}@media(max-width:520px){.cards{grid-template-columns:1fr}}
</style>
</head>
<body><main>
<h1>Immich Optimizer</h1>
<section class="cards">
<div class="card"><div class="label">Files Processed</div><div class="value" id="count">0</div></div>
<div class="card"><div class="label">Total Size Processed</div><div class="value" id="original">0 B</div></div>
<div class="card"><div class="label">Total Output Size</div><div class="value" id="compressed">0 B</div></div>
<div class="card"><div class="label">Space Saved</div><div class="value good" id="saved">0 B</div></div>
<div class="card"><div class="label">Total Reduction</div><div class="value good" id="reduction">0%</div></div>
</section>
<section>
<div class="panel"><h2>Recent Uploads</h2><table><thead><tr><th>Time</th><th>Profile</th><th>File</th><th>Original Size</th><th>Compressed Size</th><th>Saved</th><th>Reduction</th></tr></thead><tbody id="recent"></tbody></table></div>
</section>
<section>
<div class="panel"><h2>Current Log</h2><div id="logs"></div></div>
</section>
</main>
<script>
const size=n=>{const u=['B','KB','MB','GB','TB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return n.toFixed(i?2:0)+' '+u[i]};
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
async function refresh(){
 const [s,r,l]=await Promise.all([fetch('/api/stats').then(x=>x.json()),fetch('/api/recent').then(x=>x.json()),fetch('/api/logs').then(x=>x.json())]);
 count.textContent=s.processed_count;original.textContent=size(s.original_bytes);compressed.textContent=size(s.uploaded_bytes);saved.textContent=size(s.saved_bytes);reduction.textContent=s.reduction_percent.toFixed(1)+'%';
 recent.innerHTML=r.map(x=>'<tr><td>'+new Date(x.processed_at).toLocaleString()+'</td><td>'+esc(x.profile)+'</td><td title="'+esc(x.filename)+'">'+esc(x.filename)+'</td><td>'+size(x.original_bytes)+'</td><td>'+size(x.uploaded_bytes)+'</td><td>'+size(x.saved_bytes)+'</td><td>'+x.reduction_percent.toFixed(1)+'%</td></tr>').join('');
 logs.textContent=l.join('\n');logs.scrollTop=logs.scrollHeight;
}
refresh();setInterval(refresh,2000);
</script></body></html>`

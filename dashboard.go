package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	configs *TaskConfigRegistry
	logs    *LogBuffer
	logger  io.Writer
	server  *http.Server
}

func NewDashboardServer(address string, store *StatsStore, configs *TaskConfigRegistry, logs *LogBuffer, logger io.Writer) *DashboardServer {
	dashboard := &DashboardServer{
		address: address,
		store:   store,
		configs: configs,
		logs:    logs,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", dashboard.handleIndex)
	mux.HandleFunc("GET /api/stats", dashboard.handleStats)
	mux.HandleFunc("GET /api/recent", dashboard.handleRecent)
	mux.HandleFunc("DELETE /api/recent/{id}", dashboard.handleDeleteRecent)
	mux.HandleFunc("GET /api/logs", dashboard.handleLogs)
	mux.HandleFunc("GET /api/task-configs", dashboard.handleTaskConfigs)
	mux.HandleFunc("PUT /api/task-configs/{profile}", dashboard.handleSelectTaskConfig)
	mux.HandleFunc("GET /health", dashboard.handleHealth)
	dashboard.server = &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return dashboard
}

func (dashboard *DashboardServer) handleTaskConfigs(w http.ResponseWriter, _ *http.Request) {
	configs, err := dashboard.configs.List()
	dashboard.writeJSON(w, configs, err)
}

func (dashboard *DashboardServer) handleSelectTaskConfig(w http.ResponseWriter, request *http.Request) {
	var selection struct {
		Config string `json:"config"`
	}
	if err := json.NewDecoder(request.Body).Decode(&selection); err != nil || strings.TrimSpace(selection.Config) == "" {
		http.Error(w, "config is required", http.StatusBadRequest)
		return
	}
	if err := dashboard.configs.Select(request.PathValue("profile"), selection.Config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (dashboard *DashboardServer) handleRecent(w http.ResponseWriter, request *http.Request) {
	page, _ := strconv.Atoi(request.URL.Query().Get("page"))
	jobs, err := dashboard.store.RecentPage(page, 10)
	dashboard.writeJSON(w, jobs, err)
}

func (dashboard *DashboardServer) handleDeleteRecent(w http.ResponseWriter, request *http.Request) {
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}
	if err := dashboard.store.Delete(id); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
:root{color-scheme:dark;--bg:#0b1020;--panel:#141b2d;--line:#28324a;--text:#eef2ff;--muted:#94a3b8;--accent:#818cf8;--danger:#fca5a5;--danger-bg:#3f1d2a}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px system-ui,sans-serif}
main{max-width:1600px;margin:auto;padding:24px}h1{margin:0 0 20px;font-size:24px}.cards{display:grid;grid-template-columns:repeat(5,1fr);gap:14px}
.card,.panel{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:18px}.label{color:var(--muted);font-size:12px;text-transform:uppercase}.value{font-size:28px;font-weight:700;margin-top:8px}
.dashboard-section{margin-top:24px}.panel{overflow:hidden}.panel-header{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:14px}.panel h2{font-size:15px;margin:0}
button,select{border:1px solid var(--line);border-radius:7px;background:#1e293b;color:var(--text);padding:7px 12px;font:inherit}button{cursor:pointer}button:hover{background:#273449}button:focus-visible,select:focus-visible{outline:2px solid var(--accent);outline-offset:2px}.config-select{min-width:320px}.config-status{color:var(--muted)}.delete-job{padding:4px 8px;color:var(--danger)}.job-failed{background:var(--danger-bg);color:var(--danger)}.pagination{display:flex;align-items:center;justify-content:flex-end;gap:10px;margin-top:14px}.pagination button:disabled{opacity:.45;cursor:default}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:9px;border-bottom:1px solid var(--line)}th{color:var(--muted);font-size:11px;text-transform:uppercase}
#logs{height:440px;overflow:auto;white-space:pre-wrap;font:12px ui-monospace,monospace;color:#cbd5e1}.good{color:#86efac}
@media(max-width:1100px){.cards{grid-template-columns:repeat(3,1fr)}}@media(max-width:700px){.cards{grid-template-columns:repeat(2,1fr)}}@media(max-width:520px){.cards{grid-template-columns:1fr}}
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
<section class="dashboard-section">
<div class="panel"><div class="panel-header"><h2>Task Configurations</h2><span class="config-status" id="config-status"></span></div><table><thead><tr><th>Profile</th><th>Bundled Config</th><th></th></tr></thead><tbody id="task-configs"></tbody></table></div>
</section>
<section class="dashboard-section">
<div class="panel"><div class="panel-header"><h2>Recent Jobs</h2></div><table><thead><tr><th>Time</th><th>Profile</th><th>File</th><th>Resolution</th><th>Original Size</th><th>Compressed Size</th><th>Saved</th><th>Action</th><th>Reduction</th></tr></thead><tbody id="recent"></tbody></table><div class="pagination"><button id="previous-page" type="button">Previous</button><span id="page-label">Page 1 of 1</span><button id="next-page" type="button">Next</button></div></div>
</section>
<section class="dashboard-section">
<div class="panel"><div class="panel-header"><h2>Current Log</h2><button id="copy-log" type="button">Copy Log</button></div><div id="logs"></div></div>
</section>
</main>
<script>
const size=n=>{const u=['B','KB','MB','GB','TB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return n.toFixed(i?2:0)+' '+u[i]};
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const copyLog=document.getElementById('copy-log');
const taskConfigs=document.getElementById('task-configs'),configStatus=document.getElementById('config-status');
const previousPage=document.getElementById('previous-page'),nextPage=document.getElementById('next-page'),pageLabel=document.getElementById('page-label');
let currentPage=1;
copyLog.addEventListener('click',async()=>{
 try{
  await navigator.clipboard.writeText(logs.textContent);
 }catch{
  const selection=window.getSelection(),range=document.createRange();
  range.selectNodeContents(logs);selection.removeAllRanges();selection.addRange(range);
  document.execCommand('copy');selection.removeAllRanges();
 }
 copyLog.textContent='Copied';
 setTimeout(()=>copyLog.textContent='Copy Log',1500);
});
taskConfigs.addEventListener('click',async event=>{
 const button=event.target.closest('.apply-config');if(!button)return;
 const select=document.getElementById('config-'+button.dataset.index);
 configStatus.textContent='Applying...';
 const response=await fetch('/api/task-configs/'+encodeURIComponent(button.dataset.profile),{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({config:select.value})});
 configStatus.textContent=response.ok?'Applied to future jobs':(await response.text()).trim();
 await refreshTaskConfigs();
});
previousPage.addEventListener('click',()=>{if(currentPage>1){currentPage--;refresh()}});
nextPage.addEventListener('click',()=>{currentPage++;refresh()});
recent.addEventListener('click',async event=>{
 const button=event.target.closest('.delete-job');if(!button)return;
 if(!confirm('Delete this dashboard record?'))return;
 const response=await fetch('/api/recent/'+button.dataset.id,{method:'DELETE'});
 if(response.ok)refresh();
});
async function refreshTaskConfigs(){
 if(document.activeElement&&document.activeElement.classList.contains('config-select'))return;
 const profiles=await fetch('/api/task-configs').then(x=>x.json());
 taskConfigs.innerHTML=profiles.map((p,i)=>'<tr><td>'+esc(p.profile)+'</td><td><select class="config-select" id="config-'+i+'">'+p.configs.map(c=>'<option value="'+esc(c.name)+'"'+(c.name===p.current?' selected':'')+'>'+esc(c.name)+'</option>').join('')+'</select></td><td><button class="apply-config" data-index="'+i+'" data-profile="'+esc(p.profile)+'" type="button">Apply</button></td></tr>').join('');
}
async function refresh(){
 const [s,r,l]=await Promise.all([fetch('/api/stats').then(x=>x.json()),fetch('/api/recent?page='+currentPage).then(x=>x.json()),fetch('/api/logs').then(x=>x.json()),refreshTaskConfigs()]);
 count.textContent=s.processed_count;original.textContent=size(s.original_bytes);compressed.textContent=size(s.uploaded_bytes);saved.textContent=size(s.saved_bytes);reduction.textContent=s.reduction_percent.toFixed(1)+'%';
 if(currentPage>r.total_pages){currentPage=r.total_pages;return refresh()}
 recent.innerHTML=r.jobs.map(x=>'<tr class="'+(x.success?'':'job-failed')+'" title="'+esc(x.error||'')+'"><td>'+new Date(x.processed_at).toLocaleString()+'</td><td>'+esc(x.profile)+'</td><td title="'+esc(x.filename)+'">'+esc(x.filename)+'</td><td>'+(x.resolution?esc(x.resolution):'-')+'</td><td>'+size(x.original_bytes)+'</td><td>'+(x.success?size(x.uploaded_bytes):'-')+'</td><td>'+(x.success?size(x.saved_bytes):'-')+'</td><td><button class="delete-job" data-id="'+x.id+'" type="button">Delete</button></td><td>'+(x.success?x.reduction_percent.toFixed(1)+'%':'-')+'</td></tr>').join('');
 pageLabel.textContent='Page '+r.page+' of '+r.total_pages;previousPage.disabled=r.page<=1;nextPage.disabled=r.page>=r.total_pages;
 const logText=l.join('\n');
 if(logs.textContent!==logText){
  const followLog=logs.scrollHeight-logs.scrollTop-logs.clientHeight<30;
  logs.textContent=logText;
  if(followLog)logs.scrollTop=logs.scrollHeight;
 }
}
refresh();setInterval(refresh,2000);
</script></body></html>`

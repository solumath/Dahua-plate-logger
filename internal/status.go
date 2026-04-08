package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	neturl "net/url"
	"sync"
	"time"
)

type cameraEntry struct {
	Name string
	URL  string
}

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html><html><head>
<meta charset="utf-8">
<title>Plate Logger — Stav</title>
<style>
body{font-family:sans-serif;padding:20px;max-width:960px}
h1{margin-bottom:4px}
table{border-collapse:collapse;width:100%;margin-top:16px}
th,td{padding:8px 12px;text-align:left;border:1px solid #ddd}
th{background:#f5f5f5;font-weight:600}
.connected{background:#d4edda}
.error,.connecting,.pending{background:#f8d7da}
a{color:inherit}
form{margin-top:24px;display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap}
label{display:flex;flex-direction:column;gap:4px;font-size:14px}
input[type=date]{padding:6px 8px;border:1px solid #ccc;border-radius:4px}
button[type=submit]{padding:7px 16px;background:#0d6efd;color:#fff;border:none;border-radius:4px;cursor:pointer}
button[type=submit]:hover{background:#0b5ed7}
</style></head><body>
<h1>Plate Logger — Stav</h1>
<p id="uptime"></p>
<table>
<tr><th>Kamera</th><th>Stav</th><th>Poslední SPZ</th></tr>
{{range .}}<tr data-camera="{{.Name}}"><td><a href="{{.URL}}">{{.Name}}</a></td><td class="status"></td><td class="plate"></td></tr>
{{end}}</table>
<form action="/export" method="get">
  <label>Od <input type="date" id="from" name="from"></label>
  <label>Do <input type="date" id="to" name="to"></label>
  <button type="submit">Stáhnout CSV</button>
</form>
<h2 style="margin-top:32px">Poslední záznamy</h2>
<table id="plates-table">
<tr><th>Datum a čas</th><th>Kamera</th><th>SPZ</th></tr>
</table>
<script>
const today = new Date().toISOString().slice(0,10);
document.getElementById('from').value = today;
document.getElementById('to').value = today;

async function refreshCameras() {
  try {
    const d = await fetch('/status').then(r => r.json());
    document.getElementById('uptime').textContent = 'Uptime: ' + d.uptime;
    for (const cam of d.cameras) {
      const row = document.querySelector('tr[data-camera="' + cam.camera + '"]');
      if (!row) continue;
      row.className = cam.state;
      let statusText;
      switch (cam.state) {
        case 'connected':  statusText = 'Připojeno'; break;
        case 'connecting': statusText = 'Připojování #' + (cam.attempt || 1); break;
        case 'error':      statusText = cam.error || 'Chyba'; break;
        default:           statusText = 'Čekání';
      }
      row.querySelector('.status').textContent = statusText;
      row.querySelector('.plate').textContent = cam.last_plate ? new Date(cam.last_plate).toLocaleString() : '-';
    }
  } catch (_) {}
}

async function refreshPlates() {
  try {
    const rows = await fetch('/plates').then(r => r.json());
    const table = document.getElementById('plates-table');
    while (table.rows.length > 1) table.deleteRow(1);
    for (const row of rows) {
      const tr = table.insertRow();
      [row.datetime, row.camera, row.plate].forEach(text => { tr.insertCell().textContent = text; });
    }
  } catch (_) {}
}

refreshCameras();
setInterval(refreshCameras, 2000);
refreshPlates();
setInterval(refreshPlates, 10000);
</script>
</body></html>`))

type cameraStatus struct {
	mu        sync.RWMutex
	url       string // display URL: scheme://host
	state     string // "pending" | "connecting" | "connected" | "error"
	attempt   int
	lastErr   string
	lastPlate time.Time
}

func (s *cameraStatus) setConnecting(attempt int) {
	s.mu.Lock()
	s.state = "connecting"
	s.attempt = attempt
	s.lastErr = ""
	s.mu.Unlock()
}

func (s *cameraStatus) setConnected() {
	s.mu.Lock()
	s.state = "connected"
	s.attempt = 0
	s.lastErr = ""
	s.mu.Unlock()
}

func (s *cameraStatus) setError(err string) {
	s.mu.Lock()
	s.state = "error"
	s.lastErr = err
	s.mu.Unlock()
}

func (s *cameraStatus) touchPlate() {
	s.mu.Lock()
	s.lastPlate = time.Now()
	s.mu.Unlock()
}

// StatusServer tracks stream connection state and serves a status page.
type StatusServer struct {
	started  time.Time
	names    []string
	statuses map[string]*cameraStatus
	store    *Store
}

func NewStatusServer(streams []*CameraStream, store *Store) *StatusServer {
	hs := &StatusServer{
		started:  time.Now(),
		statuses: make(map[string]*cameraStatus, len(streams)),
		store:    store,
	}
	for _, s := range streams {
		cfg := s.Config()
		displayURL := cfg.URL
		if u, err := neturl.Parse(cfg.URL); err == nil {
			displayURL = u.Scheme + "://" + u.Host
		}
		hs.names = append(hs.names, s.Name())
		hs.statuses[s.Name()] = &cameraStatus{url: displayURL, state: "pending"}
	}
	return hs
}

// Touch records a plate event for the named camera.
func (hs *StatusServer) Touch(camera string) {
	if s, ok := hs.statuses[camera]; ok {
		s.touchPlate()
	}
}

// SetConnecting marks the camera as attempting to connect.
func (hs *StatusServer) SetConnecting(camera string, attempt int) {
	if s, ok := hs.statuses[camera]; ok {
		s.setConnecting(attempt)
	}
}

// SetConnected marks the camera stream as live.
func (hs *StatusServer) SetConnected(camera string) {
	if s, ok := hs.statuses[camera]; ok {
		s.setConnected()
	}
}

// SetError records a stream connection error for the camera.
func (hs *StatusServer) SetError(camera string, err error) {
	if s, ok := hs.statuses[camera]; ok {
		s.setError(err.Error())
	}
}

func (hs *StatusServer) serveIndex(w http.ResponseWriter, r *http.Request) {
	slog.Info("status page opened", "remote", r.RemoteAddr)

	cameras := make([]cameraEntry, len(hs.names))
	for i, name := range hs.names {
		s := hs.statuses[name]
		s.mu.RLock()
		cameras[i] = cameraEntry{Name: name, URL: s.url}
		s.mu.RUnlock()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, cameras); err != nil {
		slog.Error("index template", "err", err)
	}
}

func (hs *StatusServer) serveStatus(w http.ResponseWriter, _ *http.Request) {
	type camStatus struct {
		Camera    string `json:"camera"`
		State     string `json:"state"`
		Attempt   int    `json:"attempt,omitempty"`
		Error     string `json:"error,omitempty"`
		LastPlate string `json:"last_plate,omitempty"`
	}

	statuses := make([]camStatus, len(hs.names))
	for i, name := range hs.names {
		s := hs.statuses[name]
		s.mu.RLock()
		cs := camStatus{Camera: name, State: s.state, Attempt: s.attempt, Error: s.lastErr}
		if !s.lastPlate.IsZero() {
			cs.LastPlate = s.lastPlate.UTC().Format(time.RFC3339)
		}
		s.mu.RUnlock()
		statuses[i] = cs
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"uptime":  time.Since(hs.started).Round(time.Second).String(),
		"cameras": statuses,
	})
}

func (hs *StatusServer) servePlates(w http.ResponseWriter, _ *http.Request) {
	rows, err := hs.store.QueryRecent(100)
	if err != nil {
		slog.Error("plates query failed", "err", err)
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []PlateRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func (hs *StatusServer) serveExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")

	loc := time.Now().Location()
	var from, to time.Time
	if t, err := time.ParseInLocation("2006-01-02", fromStr, loc); err == nil {
		from = t
	}
	if t, err := time.ParseInLocation("2006-01-02", toStr, loc); err == nil {
		to = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, loc)
	}

	filename := "plates_" + fromStr
	if toStr != "" && toStr != fromStr {
		filename += "_to_" + toStr
	}
	filename += ".csv"

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := ExportCSV(hs.store, from, to, w); err != nil {
		slog.Error("export failed", "err", err)
	}
}

// Start launches the HTTP server on the given port in background goroutines
// and shuts it down when ctx is canceled.
func (hs *StatusServer) Start(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", hs.serveIndex)
	mux.HandleFunc("/status", hs.serveStatus)
	mux.HandleFunc("/plates", hs.servePlates)
	mux.HandleFunc("/export", hs.serveExport)

	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go func() {
		slog.Info("status server listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("status server", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck
	}()
}

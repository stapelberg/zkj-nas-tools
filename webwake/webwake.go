package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gokrazy/gokrazy"
	"github.com/stapelberg/zkj-nas-tools/internal/wake"
	"golang.org/x/sync/errgroup"
)

type httpErr struct {
	code int
	err  error
}

func (h *httpErr) Error() string {
	return h.err.Error()
}

func httpError(code int, err error) error {
	return &httpErr{code, err}
}

func handleError(h func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}
		if err == context.Canceled {
			return // client canceled the request
		}
		code := http.StatusInternalServerError
		unwrapped := err
		if he, ok := err.(*httpErr); ok {
			code = he.code
			unwrapped = he.err
		}
		log.Printf("%s: HTTP %d %s", r.URL.Path, code, unwrapped)
		http.Error(w, unwrapped.Error(), code)
	})
}

var indexTmpl = template.Must(template.New("").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <title>webwake</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="apple-mobile-web-app-capable" content="yes">
  <style>
:root {
  --color-success: #22c55e;
  --color-pending: #6b7280;
  --color-active: #eab308;
  --color-error: #ef4444;
  --color-bg: #111827;
  --color-surface: #1f2937;
  --color-text: #f9fafb;
  --color-text-dim: #9ca3af;
}

* {
  box-sizing: border-box;
}

body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: var(--color-bg);
  color: var(--color-text);
  margin: 0;
  padding: 16px;
  min-height: 100vh;
}

h1 {
  font-size: 1.5rem;
  margin: 0 0 20px 0;
  font-weight: 500;
}

.machine-btn {
  display: block;
  width: 100%;
  padding: 24px;
  margin: 12px 0;
  font-size: 1.5rem;
  min-height: 80px;
  border: none;
  border-radius: 12px;
  background: var(--color-surface);
  color: var(--color-text);
  cursor: pointer;
  transition: transform 0.1s, background 0.1s;
  -webkit-tap-highlight-color: transparent;
}

.machine-btn:active {
  transform: scale(0.98);
  background: #374151;
}

#progress-view {
  display: none;
}

#progress-view.active {
  display: block;
}

#machine-list.hidden {
  display: none;
}

.header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 24px;
}

#back-btn {
  background: none;
  border: none;
  color: var(--color-text-dim);
  font-size: 1rem;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: 8px;
  -webkit-tap-highlight-color: transparent;
}

#back-btn:active {
  background: var(--color-surface);
}

#target-name {
  font-size: 1.5rem;
  margin: 0;
  font-weight: 500;
}

.phase-row {
  display: grid;
  grid-template-columns: 2rem 1fr auto;
  gap: 12px;
  padding: 16px 0;
  border-bottom: 1px solid #374151;
  align-items: start;
}

.phase-row:last-child {
  border-bottom: none;
}

.phase-symbol {
  font-size: 1.25rem;
  text-align: center;
}

.phase-content {
  min-width: 0;
}

.phase-label {
  font-size: 1.1rem;
  margin-bottom: 4px;
}

.phase-detail {
  font-size: 0.9rem;
  color: var(--color-text-dim);
  word-break: break-word;
}

.phase-time {
  font-size: 0.95rem;
  color: var(--color-text-dim);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.status-pending .phase-symbol,
.status-pending .phase-label { color: var(--color-pending); }
.status-start .phase-symbol,
.status-start .phase-label { color: var(--color-active); }
.status-done .phase-symbol,
.status-done .phase-label { color: var(--color-success); }
.status-error .phase-symbol,
.status-error .phase-label { color: var(--color-error); }
.status-skipped .phase-symbol,
.status-skipped .phase-label { color: var(--color-pending); }

#total {
  margin-top: 24px;
  padding-top: 16px;
  border-top: 2px solid #374151;
  font-size: 1.1rem;
  color: var(--color-text-dim);
}
  </style>
</head>
<body>

<div id="machine-list">
  <h1>webwake</h1>
  {{ range $machine := .Machines }}
  <button class="machine-btn" data-machine="{{ $machine }}">{{ $machine }}</button>
  {{ end }}
</div>

<div id="progress-view">
  <div class="header">
    <button id="back-btn">← Back</button>
    <h2 id="target-name"></h2>
  </div>
  <div id="phases"></div>
  <div id="total">Total: -</div>
</div>

<script>
const PHASES = [
  { name: 'checking', label: 'Checking' },
  { name: 'waking', label: 'Waking' },
  { name: 'ssh', label: 'Waiting for SSH' },
  { name: 'health', label: 'Health check' }
];

const SPINNER = ['◐', '◓', '◑', '◒'];

let eventSource = null;
let spinnerInterval = null;
let spinnerFrame = 0;
let startTime = null;
let phaseState = {};

function formatDuration(ms) {
  if (ms == null || ms === 0) return '-';
  if (ms < 1000) return ms + 'ms';
  return (ms / 1000).toFixed(1) + 's';
}

function getSymbol(status, frame) {
  switch (status) {
    case 'done': return '✓';
    case 'start': return SPINNER[frame % SPINNER.length];
    case 'error': return '✗';
    case 'skipped': return '○';
    default: return '○';
  }
}

function renderPhases() {
  const container = document.getElementById('phases');
  container.innerHTML = PHASES.map(p => {
    const state = phaseState[p.name] || { status: 'pending', detail: '', elapsed: null };
    const symbol = getSymbol(state.status, spinnerFrame);
    return ` + "`" + `
      <div class="phase-row status-${state.status}">
        <div class="phase-symbol">${symbol}</div>
        <div class="phase-content">
          <div class="phase-label">${p.label}</div>
          <div class="phase-detail">${state.detail || ''}</div>
        </div>
        <div class="phase-time">${formatDuration(state.elapsed)}</div>
      </div>
    ` + "`" + `;
  }).join('');
}

function updateTotal(done) {
  const el = document.getElementById('total');
  const elapsed = Date.now() - startTime;
  el.textContent = 'Total: ' + formatDuration(elapsed) + (done ? '' : '...');
}

function resetState() {
  phaseState = {};
  spinnerFrame = 0;
  startTime = Date.now();
}

function startWake(machine) {
  resetState();

  document.getElementById('machine-list').classList.add('hidden');
  document.getElementById('progress-view').classList.add('active');
  document.getElementById('target-name').textContent = machine;

  renderPhases();
  updateTotal(false);

  spinnerInterval = setInterval(() => {
    spinnerFrame++;
    renderPhases();
    updateTotal(false);
  }, 100);

  eventSource = new EventSource('/wake/stream?machine=' + encodeURIComponent(machine));

  eventSource.onmessage = (e) => {
    const event = JSON.parse(e.data);

    if (event.phase !== 'complete') {
      phaseState[event.phase] = {
        status: event.status,
        detail: event.detail || '',
        elapsed: event.elapsed_ms || null
      };
    }

    if (event.phase === 'complete') {
      clearInterval(spinnerInterval);
      spinnerInterval = null;
      eventSource.close();
      eventSource = null;
      updateTotal(true);
    }

    renderPhases();
  };

  eventSource.onerror = () => {
    clearInterval(spinnerInterval);
    spinnerInterval = null;
    if (eventSource) {
      eventSource.close();
      eventSource = null;
    }
  };
}

function goBack() {
  if (spinnerInterval) {
    clearInterval(spinnerInterval);
    spinnerInterval = null;
  }
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  document.getElementById('progress-view').classList.remove('active');
  document.getElementById('machine-list').classList.remove('hidden');
}

document.querySelectorAll('.machine-btn').forEach(btn => {
  btn.addEventListener('click', () => startWake(btn.dataset.machine));
});

document.getElementById('back-btn').addEventListener('click', goBack);
</script>

</body>
</html>`))

var wokenTmpl = template.Must(template.New("").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<title>webwake</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
  body {
    font-size: 200%;
  }
  </style>
</head>
<body>

<pre>
{{ .Message }}
</pre>

</body>
</html>`))

type server struct {
	mqttBroker string
}

var hostname = func() string {
	host, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	return host
}()

func (s *server) index(w http.ResponseWriter, r *http.Request) error {
	filtered := make([]string, 0, len(wake.Hosts))
	for _, host := range wake.Hosts {
		if host.Relay != hostname {
			continue
		}
		filtered = append(filtered, host.Name)
	}
	sort.Strings(filtered)
	var buf bytes.Buffer
	if err := indexTmpl.Execute(&buf, struct {
		Machines []string
	}{
		Machines: filtered,
	}); err != nil {
		return err
	}
	_, err := io.Copy(w, &buf)
	return err
}

func (s *server) wake(w http.ResponseWriter, r *http.Request) error {
	// Shelly Button can only send a GET request
	// if r.Method != "POST" {
	// 	return httpError(http.StatusBadRequest, fmt.Errorf("invalid method"))
	// }

	host := r.FormValue("machine")
	if host == "" {
		return httpError(http.StatusBadRequest, fmt.Errorf("no host parameter"))
	}

	log.Printf("wake(%s)", host)

	target, ok := wake.Hosts[host]
	if !ok {
		return httpError(http.StatusNotFound, fmt.Errorf("host not found"))
	}
	cfg := wake.Config{
		MQTTBroker: s.mqttBroker,
		ClientID:   "github.com/stapelberg/zkj-nas-tools/webwake",
		Target:     target,
	}
	message := "waking up…"
	err := cfg.Wakeup(context.Background())
	if err == wake.ErrAlreadyRunning {
		message = host + " already running"
	} else if err != nil {
		message = err.Error()
	}

	var buf bytes.Buffer
	if err := wokenTmpl.Execute(&buf, struct {
		Message string
	}{
		Message: message,
	}); err != nil {
		return err
	}
	_, err = io.Copy(w, &buf)
	return err
}

// wol sends a Wake-on-LAN packet (or MQTT for storage2) and returns immediately.
// This is an atomic building block for CLI orchestration.
func (s *server) wol(w http.ResponseWriter, r *http.Request) error {
	host := r.FormValue("machine")
	if host == "" {
		return httpError(http.StatusBadRequest, fmt.Errorf("no machine parameter"))
	}

	log.Printf("wol(%s)", host)

	target, ok := wake.Hosts[host]
	if !ok {
		return httpError(http.StatusNotFound, fmt.Errorf("host not found"))
	}

	if target.Relay != hostname {
		return httpError(http.StatusBadRequest, fmt.Errorf("machine %s is served by relay %s, not %s", host, target.Relay, hostname))
	}

	cfg := wake.Config{
		MQTTBroker: s.mqttBroker,
		ClientID:   "github.com/stapelberg/zkj-nas-tools/webwake",
		Target:     target,
	}

	if err := cfg.SendWakeSignal(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// pollSSH streams SSE events while polling SSH port, completing when SSH responds.
// This is an atomic building block for CLI orchestration.
func (s *server) pollSSH(w http.ResponseWriter, r *http.Request) error {
	host := r.FormValue("machine")
	if host == "" {
		return httpError(http.StatusBadRequest, fmt.Errorf("no machine parameter"))
	}

	log.Printf("pollSSH(%s)", host)

	target, ok := wake.Hosts[host]
	if !ok {
		return httpError(http.StatusNotFound, fmt.Errorf("host not found"))
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpError(http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
	}

	startTime := time.Now()

	sendEvent := func(event progressEvent) {
		event.ElapsedMs = time.Since(startTime).Milliseconds()
		data, err := json.Marshal(event)
		if err != nil {
			log.Printf("failed to marshal event: %v", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	sendEvent(progressEvent{
		Phase:  "ssh",
		Status: "start",
		Detail: fmt.Sprintf("polling tcp/22 on %s", target.Name),
	})

	sshCtx, canc := context.WithTimeout(r.Context(), 5*time.Minute)
	defer canc()

	if err := wake.PollSSH(sshCtx, target.IP+":22"); err != nil {
		sendEvent(progressEvent{
			Phase:  "ssh",
			Status: "error",
			Detail: err.Error(),
		})
		return nil
	}

	sendEvent(progressEvent{
		Phase:  "ssh",
		Status: "done",
		Detail: "ssh responding",
	})

	return nil
}

// progressEvent is a Server-Sent Event to report wake progress.
type progressEvent struct {
	Phase     string `json:"phase"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
}

func (s *server) wakeStream(w http.ResponseWriter, r *http.Request) error {
	host := r.FormValue("machine")
	if host == "" {
		return httpError(http.StatusBadRequest, fmt.Errorf("no host parameter"))
	}

	log.Printf("wakeStream(%s)", host)

	target, ok := wake.Hosts[host]
	if !ok {
		return httpError(http.StatusNotFound, fmt.Errorf("host not found"))
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		return httpError(http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
	}

	cfg := wake.Config{
		MQTTBroker: s.mqttBroker,
		ClientID:   "github.com/stapelberg/zkj-nas-tools/webwake",
		Target:     target,
	}

	startTime := time.Now()
	phaseStart := time.Now()

	sendEvent := func(event progressEvent) {
		if event.Status == "start" {
			phaseStart = time.Now() // reset for newly starting phase
		} else {
			event.ElapsedMs = time.Since(phaseStart).Milliseconds()
		}
		if event.Phase == "complete" {
			event.ElapsedMs = time.Since(startTime).Milliseconds()
		}
		data, err := json.Marshal(event)
		if err != nil {
			log.Printf("failed to marshal event: %v", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	progressFn := func(phase, status, detail string) {
		sendEvent(progressEvent{
			Phase:  phase,
			Status: status,
			Detail: detail,
		})
	}

	err := cfg.WakeupWithProgress(r.Context(), progressFn)
	if err != nil && err != wake.ErrAlreadyRunning {
		sendEvent(progressEvent{
			Phase:  "complete",
			Status: "error",
			Detail: err.Error(),
		})
	}

	return nil
}

func listenAndServe(ctx context.Context, srv *http.Server) error {
	errC := make(chan error)
	go func() {
		errC <- srv.ListenAndServe()
	}()
	select {
	case err := <-errC:
		return err
	case <-ctx.Done():
		timeout, canc := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer canc()
		_ = srv.Shutdown(timeout)
		return ctx.Err()
	}
}

func webwake() error {
	var (
		listenAddr = flag.String("listen",
			"localhost:8911,consrv.lan:8911",
			"(comma-separated list of) [host]:port HTTP listen address(es)")

		mqttBroker = flag.String("mqtt_broker",
			"tcp://dr.lan:1883",
			"MQTT broker address for github.com/eclipse/paho.mqtt.golang")
	)

	flag.Parse()

	// WaitForClock also (indirectly) ensures the network is up.
	gokrazy.WaitForClock()

	srv := &server{
		mqttBroker: *mqttBroker,
	}

	mux := http.NewServeMux()
	mux.Handle("/", handleError(srv.index))
	mux.Handle("/wake", handleError(srv.wake))
	mux.Handle("/wake/stream", handleError(srv.wakeStream))
	mux.Handle("/wol", handleError(srv.wol))
	mux.Handle("/poll/ssh", handleError(srv.pollSSH))

	eg, ctx := errgroup.WithContext(context.Background())
	for _, addr := range strings.Split(*listenAddr, ",") {
		srv := &http.Server{
			Handler: mux,
			Addr:    addr,
		}
		eg.Go(func() error {
			return listenAndServe(ctx, srv)
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := webwake(); err != nil {
		log.Fatal(err)
	}
}

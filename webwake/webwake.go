package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"maps"
	"net/http"
	"slices"
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
  <style>
  body {
    font-size: 200%;
  }
  </style>
</head>
<body>

select machine to wake up:

<form action="/wake" method="post">

<select name="machine" id="machine">
{{ range $machine := .Machines }}
<option value="{{ $machine }}" {{ if (eq $machine "storage2") }} selected="selected" {{ end }}>{{ $machine }}</option>
{{ end }}
</select><br>

<input type="submit" value="wake">

</form>

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

func (s *server) index(w http.ResponseWriter, r *http.Request) error {
	var buf bytes.Buffer
	if err := indexTmpl.Execute(&buf, struct {
		Machines []string
	}{
		Machines: slices.Sorted(maps.Keys(wake.Hosts)),
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

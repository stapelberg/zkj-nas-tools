package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/fearful-symmetry/garlic"
	"golang.org/x/sync/errgroup"

	_ "net/http/pprof"
)

var (
	listenAddress = flag.String("listen_address",
		"localhost:4000",
		"host:port on which the HTTP server will listen")

	programName = flag.String("program",
		"i3lock",
		"Name (as in /proc/<pid>/comm) of the program to monitor")

	runStatus bool
	runMu     sync.RWMutex
)

var host = func() string {
	host, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	return host
}()

var mqttClient mqtt.Client

func publishStatus(running bool) {
	jsonval := struct {
		Running bool `json:"running"`
	}{running}
	b, err := json.Marshal(jsonval)
	if err != nil {
		log.Println(err)
	} else {
		mqttClient.Publish("runstatus/"+host+"/"+*programName, 0 /* qos */, true /* retained */, string(b))
	}
}

func listenNetlink() error {
	// Only superuser is allowed to listen to multicast connector messages:
	// https://github.com/torvalds/linux/blob/2c523b344dfa65a3738e7039832044aa133c75fb/net/netlink/af_netlink.c#L992
	conn, err := garlic.DialPCNWithEvents([]garlic.EventType{
		garlic.ProcEventExec,
		garlic.ProcEventExit,
	})
	if err != nil {
		return err
	}
	var prev bool
	programPids := make(map[uint32]bool)
	for {
		data, err := conn.ReadPCN()
		if err != nil {
			return err
		}
		for _, ev := range data {
			switch x := ev.EventData.(type) {
			case garlic.Exec:
				//log.Printf("  exec: %+v", x)
				if b, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/comm", x.ProcessPid)); err == nil {
					if strings.TrimSpace(string(b)) == *programName {
						programPids[x.ProcessPid] = true
					}
				}

			case garlic.Exit:
				//log.Printf("  exit: %+v", x)
				delete(programPids, x.ProcessPid)
			}
		}
		runMu.Lock()
		runStatus = len(programPids) > 0
		if prev != runStatus {
			log.Printf("  status change: prev=%v, now=%v", prev, runStatus)
			publishStatus(runStatus)
			prev = runStatus
		}
		runMu.Unlock()
	}
}

func pollProc() error {
	for {
		var status bool
		filepath.Walk("/proc", func(path string, info os.FileInfo, err error) error {
			if path == "/proc" {
				return nil
			}
			if b, err := ioutil.ReadFile(filepath.Join(path, "comm")); err == nil {
				if strings.TrimSpace(string(b)) == *programName {
					status = true
				}
			}

			if err != nil || info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		})

		runMu.Lock()
		runStatus = status
		runMu.Unlock()

		time.Sleep(1 * time.Second)
	}
}

func main() {
	flag.Parse()

	opts := mqtt.NewClientOptions().AddBroker("tcp://dr.lan:1883")
	opts = opts.SetClientID("runstatus-" + host)
	opts = opts.SetOnConnectHandler(func(mqtt.Client) {
		runMu.RLock()
		defer runMu.RUnlock()
		log.Printf("(re)connected, publishing status: %v", runStatus)
		publishStatus(runStatus)
	})
	mqttClient = mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		// TODO: connect asynchronously in the background to avoid this hard dependency
		log.Fatalf("MQTT connection failed: %v", token.Error())
	}

	ctx := context.Background()
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(listenNetlink)
	// eg.Go(pollProc)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		runMu.RLock()
		status := runStatus
		runMu.RUnlock()
		if status {
			fmt.Fprintf(w, "running")
		} else {
			fmt.Fprintf(w, "notrunning")
		}
	})
	eg.Go(func() error {
		srv := &http.Server{
			Addr: *listenAddress,
		}
		serveErr := make(chan error)
		go func() {
			serveErr <- srv.ListenAndServe()
		}()
		select {
		case <-ctx.Done():
			return srv.Shutdown(ctx)
		case err := <-serveErr:
			return err
		}
	})
	if err := eg.Wait(); err != nil {
		log.Fatal(err)
	}
}

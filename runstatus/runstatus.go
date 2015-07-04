package main

import (
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
)

var (
	listenAddress = flag.String("listen_address",
		"localhost:4000",
		"host:port on which the HTTP server will listen")

	programName = flag.String("program",
		"i3lock",
		"Name (as in /proc/<pid>/comm) of the program to monitor")

	runStatus = "notrunning"
	runMu     sync.RWMutex
)

func main() {
	flag.Parse()
	go func() {
		for {
			status := "notrunning"
			filepath.Walk("/proc", func(path string, info os.FileInfo, err error) error {
				if path == "/proc" {
					return nil
				}
				if b, err := ioutil.ReadFile(filepath.Join(path, "comm")); err == nil {
					if strings.TrimSpace(string(b)) == *programName {
						status = "running"
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
	}()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		runMu.RLock()
		status := runStatus
		runMu.RUnlock()
		fmt.Fprintf(w, "%s", status)
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

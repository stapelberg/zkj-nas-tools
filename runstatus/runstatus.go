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

	"github.com/fearful-symmetry/garlic"
	"golang.org/x/sync/errgroup"
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
	var prev string
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
		if len(programPids) > 0 {
			runStatus = "running"
		} else {
			runStatus = "notrunning"
		}
		if prev != runStatus {
			log.Printf("  status change: prev=%v, now=%v", prev, runStatus)
			prev = runStatus
		}
		runMu.Unlock()
	}
}

func pollProc() error {
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
}

func main() {
	flag.Parse()

	var eg errgroup.Group
	eg.Go(listenNetlink)
	// eg.Go(pollProc)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		runMu.RLock()
		status := runStatus
		runMu.RUnlock()
		fmt.Fprintf(w, "%s", status)
	})
	eg.Go(func() error { return http.ListenAndServe(*listenAddress, nil) })
	if err := eg.Wait(); err != nil {
		log.Fatal(err)
	}
}

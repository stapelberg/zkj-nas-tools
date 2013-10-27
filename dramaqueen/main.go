// If nobody pays any attention to my samba server, I might as well power off!
//
// Parses the output of “net status sessions parseable” and pings all of the
// machines that are listed in the output. If none of the machines responds,
// dramaqueen might shut off the machine after a brief timeout.
//
// The automatic shutdown might be restricted to certain times of the day or
// inhibited entirely by calling /inhibit?inhibitor=<key>, where each <key>
// identifies the requesting program. To undo, call /release?inhibitor=<key>,
// to inspect, check /
package main

import (
	"flag"
	"fmt"
	"github.com/stapelberg/zkj-nas-tools/ping"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	netCommand = flag.String("net_command",
		"net",
		"“net” command, called as “net status sessions parseable”.")
	idleSeconds = flag.Int("idle_seconds",
		10,
		"time in seconds to wait before actually shutting down.")
	listenAddress = flag.String("listen_address",
		":4414",
		"host:port to listen on (http).")

	// Play it safe: assume somebody is using the samba server currently.
	reachableUsers bool = true
	inhibitors          = make(map[string]time.Time)
	hosts          []string
	statusLock     sync.Mutex
)

// NB: Even though it says hostname, on my machine this is an IP address.
func getSessionHostnames() []string {
	// from samba/2:3.6.16-1/source3/utils/net_status.c:
	// if (*parseable) {
	// 		d_printf("%s\\%s\\%s\\%s\\%s\n",
	// 			 procid_str_static(&session->pid),
	// 			 uidtoname(session->uid),
	// 			 gidtoname(session->gid),
	// 			 session->remote_machine, session->hostname);
	// 	}

	cmd := exec.Command(*netCommand, "status", "sessions", "parseable")
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	hostnames := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(line, "\\")
		if len(parts) < 4 {
			continue
		}
		hostnames = append(hostnames, parts[4])
	}

	return hostnames
}

// Checks periodically whether a shutdown is appropriate.
func checkShutdown() {
	possibleSince := time.Now()
	for {
		time.Sleep(1 * time.Second)

		statusLock.Lock()
		shutdownPossible := !reachableUsers && len(inhibitors) == 0
		statusLock.Unlock()

		if !shutdownPossible {
			possibleSince = time.Now()
			continue
		}

		if time.Since(possibleSince) <= time.Duration(*idleSeconds)*time.Second {
			continue
		}

		cmd := exec.Command("systemctl", "poweroff")
		err := cmd.Run()
		if err != nil {
			log.Fatal(err)
		}

		time.Sleep(60)
		log.Fatal("This program still lives 60s after triggering a shutdown.")
	}
}

// Runs infinitely as a goroutine, periodically pinging samba users.
func pingUsers() {
	for {
		statusLock.Lock()
		hosts = getSessionHostnames()
		statusLock.Unlock()

		// This default will lead to a shutdown in case the machine gets booted
		// and nobody starts using it within 10 minutes, which is intentional.
		// The machine should only be booted when usage is imminent.
		reachable := false
		if len(hosts) > 0 {
			result := make(chan *time.Duration)
			for _, host := range hosts {
				go ping.Ping(host, 5*time.Second, result)
			}
			reachable = <-result != nil
		}
		statusLock.Lock()
		reachableUsers = reachable
		statusLock.Unlock()
		time.Sleep(10 * time.Second)
	}
}

func main() {
	flag.Parse()

	go pingUsers()
	go checkShutdown()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		statusLock.Lock()
		fmt.Fprintf(w, `<html><head><meta charset="utf8"></head><body>`)
		fmt.Fprintf(w, "hosts = %v<br>\n", hosts)
		fmt.Fprintf(w, "reachable = %v", reachableUsers)
		fmt.Fprintf(w, "<h2>Inhibitors</h2><ul>")
		for key, since := range inhibitors {
			fmt.Fprintf(w, `<li>inhibitor "%s" since %v</li>`, key, since)
		}
		fmt.Fprintf(w, "</ul>")
		statusLock.Unlock()
	})

	http.HandleFunc("/inhibit", func(w http.ResponseWriter, r *http.Request) {
		key := r.FormValue("key")
		statusLock.Lock()
		inhibitors[key] = time.Now()
		statusLock.Unlock()
	})

	http.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		key := r.FormValue("key")
		statusLock.Lock()
		delete(inhibitors, key)
		statusLock.Unlock()
	})

	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

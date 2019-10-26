package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/stapelberg/zkj-nas-tools/internal/utmp"
)

type state struct {
	open func() (io.ReadCloser, error)
}

func (s *state) busy() bool {
	f, err := s.open()
	if err != nil {
		return true // err on the side of not shutting down
	}
	defer f.Close()
	var records []*utmp.Utmp
	for {
		u, err := utmp.ReadRecord(f)
		if err != nil {
			if err == io.EOF {
				break
			}
			return true
		}
		records = append(records, u)
	}
	for _, r := range records {
		if r.Type() != utmp.UserProcess {
			continue
		}
		return true // any session active (screen, local or remote)
	}
	return false // no sessions active
}

func utmpLogic(timeout, frequency time.Duration) error {
	s := state{
		open: func() (io.ReadCloser, error) { return os.Open("/var/run/utmp") },
	}
	var idleFor int
	for range time.Tick(frequency) {
		if s.busy() {
			idleFor = 0
		} else {
			idleFor++
		}
		log.Printf("idleFor = %v (poweroff at >= %v)", idleFor, int(timeout/frequency))
		if idleFor >= int(timeout/frequency) {
			poweroff := exec.Command("systemctl", "poweroff")
			poweroff.Stdout = os.Stdout
			poweroff.Stderr = os.Stderr
			log.Printf("Triggering shutdown using %v", poweroff.Args)
			if err := poweroff.Run(); err != nil {
				log.Fatalf("%v: %v", poweroff.Args, err)
			}
		}
	}
	return nil
}

func main() {
	var (
		timeout   = flag.Duration("timeout", 10*time.Minute, "idle timeout (no sessions) until the machine is powered off")
		frequency = flag.Duration("frequency", 1*time.Second, "frequency with which to check for active sesions")
	)
	flag.Parse()
	if err := utmpLogic(*timeout, *frequency); err != nil {
		log.Fatal(err)
	}
}

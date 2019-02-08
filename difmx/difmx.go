package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	listen = flag.String("listen",
		":8043",
		"[host]:port listen address")
)

const (
	serialPort          = "/dev/ttyAMA0" // Raspberry Pi pin header UART
	defaultInputChannel = 0              // midna
)

type srv struct {
	uart     *os.File
	pin      *gpio
	requests chan int

	mu      sync.Mutex
	cond    *sync.Cond
	current int64
}

func newServer() (*srv, error) {
	log.Printf("opening 57600 8N1 serial port %s", serialPort)
	uart, err := os.OpenFile(serialPort, os.O_EXCL|os.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}

	if err := ConfigureSerial(uintptr(uart.Fd())); err != nil {
		return nil, err
	}

	// Flush all data in the input buffer
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, uart.Fd(), unix.TCFLSH, uintptr(syscall.TCIFLUSH)); err != 0 {
		return nil, err
	}

	// Re-enable blocking syscalls, which are required by the Go
	// standard library.
	if err := syscall.SetNonblock(int(uart.Fd()), false); err != nil {
		return nil, err
	}

	pin, err := newGPIO()
	if err != nil {
		return nil, err
	}

	s := &srv{
		uart:     uart,
		pin:      pin,
		requests: make(chan int),
		current:  -1,
	}
	s.cond = sync.NewCond(&s.mu)
	go s.statemachine()
	s.requests <- defaultInputChannel
	return s, nil
}

func (s *srv) setCurrent(current int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = current
	s.cond.Broadcast()
}

func (s *srv) getCurrent() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *srv) awaitCurrent(val int64) {
	s.mu.Lock()
	for s.current != val {
		s.cond.Wait()
	}
	s.mu.Unlock()
}

func (s *srv) statemachine() {
	lines := make(chan string)
	scanner := bufio.NewScanner(s.uart)
	go func() {
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		log.Fatalf("read(%s): %v", serialPort, scanner.Err())
	}()

	target := 0
	for {
		select {
		case req := <-s.requests:
			target = req
			if err := s.pin.Pulse(); err != nil {
				log.Fatalf("pulse: %v", err)
			}
		case line := <-lines:
			if !strings.HasPrefix(line, "PortSel ") {
				log.Printf("ignoring unexpected line: %q", line)
				continue
			}
			current, err := strconv.ParseInt(strings.TrimPrefix(line, "PortSel "), 0, 64)
			if err != nil {
				log.Printf("malformed line %q: %v", line, err)
				continue
			}
			s.setCurrent(current)
			if int(current) != target {
				log.Printf("reached input %d, going on", current)
				if err := s.pin.Pulse(); err != nil {
					log.Fatalf("pulse: %v", err)
				}
			} else {
				log.Printf("reached target %d", target)
			}
		}
	}

}

func (s *srv) Close() error {
	return s.uart.Close()
}

func internalServerError(h func(w http.ResponseWriter, r *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			log.Printf("%s %s: %v", r.Method, r.URL, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

func logic() error {
	srv, err := newServer()
	if err != nil {
		return err
	}

	http.Handle("/select", internalServerError(func(w http.ResponseWriter, r *http.Request) error {
		targetstr := r.FormValue("target")
		if targetstr == "" {
			return fmt.Errorf(`no "target" parameter specified`)
		}
		target, err := strconv.ParseInt(targetstr, 0, 64)
		if err != nil {
			return err
		}
		if target < 0 || target > 3 {
			return fmt.Errorf("target %d out of range [0, 3]", target)
		}
		srv.requests <- int(target)
		srv.awaitCurrent(target)
		fmt.Fprintf(w, "%d\n", srv.getCurrent())
		return nil
	}))

	http.Handle("/current", internalServerError(func(w http.ResponseWriter, r *http.Request) error {
		fmt.Fprintf(w, "%d\n", srv.getCurrent())
		return nil
	}))

	log.Printf("listening on %s", *listen)
	return http.ListenAndServe(*listen, nil)
}

func main() {
	flag.Parse()
	if err := logic(); err != nil {
		log.Fatal(err)
	}
}

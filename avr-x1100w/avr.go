package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

var toAvr = make(chan string)

func talkWithAvr() {
	fromAvr := make(chan string)
	go func() {
		for r := range fromAvr {
			log.Printf("from avr: %q\n", r)

			stateMu.Lock()
			if strings.HasPrefix(r, "PW") {
				// e.g. PWSTANDBY
				state.avrPowered = r == "PWON"
			} else if strings.HasPrefix(r, "SI") {
				// e.g. SIGAME
				state.avrSource = r[len("SI"):]
			}
			lastContact["avr"] = time.Now()
			stateMu.Unlock()
			stateChanged.Broadcast()
		}
	}()
	for {
		conn, err := net.Dial("tcp", "avr:23")
		if err != nil {
			log.Print(err)
			continue
		}
		go func() {
			for {
				cmd := <-toAvr
				log.Printf("to avr: %q\n", cmd)
				if _, err := conn.Write([]byte(cmd)); err != nil {
					log.Printf("Error writing to AVR: %v\n", err)
					return
				}
			}
		}()
		fmt.Fprintf(conn, "PW?\r")
		fmt.Fprintf(conn, "SI?\r")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\r')
			if err != nil {
				break
			}
			fromAvr <- strings.TrimRight(line, "\r")
		}
		conn.Close()
	}
}

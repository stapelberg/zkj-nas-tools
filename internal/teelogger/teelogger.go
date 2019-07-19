// Package teelogger provides loggers which send their output to multiple
// writers, like the tee(1) command.
package teelogger

import (
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
)

// NewRemoteSyslog returns a logger which writes logs to midna.lan:514 (UDP
// remote syslog) and os.Stderr.
func NewRemoteSyslog() *log.Logger {
	// This is what midna.lan:514 resolves to, as DNS may not yet be available
	// in early boot, resulting in failed dials.
	const raddr = "10.0.0.76:514"

	var w io.Writer
	w, err := syslog.Dial("udp", raddr, syslog.LOG_INFO, "gokrazy-dr")
	if err != nil {
		log.Printf("dialing %s: %v", raddr, err)
		w = ioutil.Discard
	}

	return log.New(io.MultiWriter(os.Stderr, w), "", log.LstdFlags|log.Lshortfile)
}

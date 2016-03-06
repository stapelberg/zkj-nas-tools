//+build linux

package serial

import (
	"syscall"
	"unsafe"
)

// From linux/include/uapi/asm-generic/termbits.h
const (
	CBAUD   = 0010017
	CBAUDEX = 0010000
	CRTSCTS = 020000000000
)

type fder interface {
	Fd() uintptr
}

// Configure configures f as a 8N1 serial port with the specified baudrate.
func Configure(f fder, baudrate uint32) error {
	var termios syscall.Termios
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(f.Fd()), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios))); err != 0 {
		return err
	}

	termios.Ispeed = baudrate
	termios.Ospeed = baudrate
	termios.Cflag &^= CBAUD
	termios.Cflag &^= CBAUDEX
	termios.Cflag |= baudrate

	// set 8N1
	termios.Cflag &^= syscall.PARENB
	termios.Cflag &^= syscall.CSTOPB
	termios.Cflag &^= syscall.CSIZE
	termios.Cflag |= syscall.CS8

	// Local connection, no modem control
	termios.Cflag |= (syscall.CLOCAL | syscall.CREAD)
	// Disable hardware flow control
	termios.Cflag &^= CRTSCTS
	// Block on a zero read (instead of returning EOF)
	termios.Cc[syscall.VMIN] = 1

	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(f.Fd()), syscall.TCSETS, uintptr(unsafe.Pointer(&termios))); err != 0 {
		return err
	}
	return nil
}

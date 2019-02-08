//+build linux

package main

import (
	"syscall"
	"unsafe"
)

// ConfigureSerial configures fd as a 57600 baud 8N1 serial port.
func ConfigureSerial(fd uintptr) error {
	var termios syscall.Termios
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios))); err != 0 {
		return err
	}

	termios.Iflag = 0
	termios.Oflag = 0
	termios.Lflag = 0
	termios.Ispeed = syscall.B57600
	termios.Ospeed = syscall.B57600
	termios.Cflag = syscall.B57600 | syscall.CS8 | syscall.CREAD

	// Block on a zero read (instead of returning EOF)
	termios.Cc[syscall.VMIN] = 1
	termios.Cc[syscall.VTIME] = 0

	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&termios))); err != 0 {
		return err
	}

	return nil
}

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	GPIOHANDLE_REQUEST_OUTPUT        = 0x2
	GPIO_GET_LINEHANDLE_IOCTL        = 0xc16cb403
	GPIOHANDLE_SET_LINE_VALUES_IOCTL = 0xc040b409
)

type gpiohandlerequest struct {
	Lineoffsets   [64]uint32
	Flags         uint32
	DefaultValues [64]uint8
	ConsumerLabel [32]byte
	Lines         uint32
	Fd            uintptr
}

type gpiohandledata struct {
	Values [64]uint8
}

type gpio struct {
	f        *os.File
	handlefd uintptr
}

func newGPIO() (*gpio, error) {
	f, err := os.Open("/dev/gpiochip0")
	if err != nil {
		return nil, err
	}

	handlereq := gpiohandlerequest{
		Lineoffsets:   [64]uint32{6}, // BCM6, pin 31
		Flags:         GPIOHANDLE_REQUEST_OUTPUT,
		DefaultValues: [64]uint8{1},
		ConsumerLabel: [32]byte{'d', 'i', 'f', 'm', 'x'},
		Lines:         1,
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(f.Fd()), GPIO_GET_LINEHANDLE_IOCTL, uintptr(unsafe.Pointer(&handlereq))); errno != 0 {
		return nil, errno
	}

	return &gpio{
		f:        f,
		handlefd: uintptr(handlereq.Fd),
	}, nil
}

func (g *gpio) Close() error {
	return g.f.Close()
}

// Pulse sends a “select” pulse via the GPIO pin
func (g *gpio) Pulse() error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, g.handlefd, GPIOHANDLE_SET_LINE_VALUES_IOCTL, uintptr(unsafe.Pointer(&gpiohandledata{
		Values: [64]uint8{0},
	}))); errno != 0 {
		return errno
	}
	time.Sleep(250 * time.Millisecond)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, g.handlefd, GPIOHANDLE_SET_LINE_VALUES_IOCTL, uintptr(unsafe.Pointer(&gpiohandledata{
		Values: [64]uint8{1},
	}))); errno != 0 {
		return errno
	}
	time.Sleep(250 * time.Millisecond)

	return nil
}

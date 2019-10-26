package utmp

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
)

// based on github.com/neko-neko/utmpdump

// get byte \0 index
func getByteLen(byteArray []byte) int {
	n := bytes.IndexByte(byteArray[:], 0)
	if n == -1 {
		return 0
	}

	return n
}

const (
	Empty        = 0x0
	RunLevel     = 0x1
	BootTime     = 0x2
	NewTime      = 0x3
	OldTime      = 0x4
	InitProcess  = 0x5
	LoginProcess = 0x6
	UserProcess  = 0x7
	DeadProcess  = 0x8
	Accounting   = 0x9
)

const (
	LineSize = 32
	NameSize = 32
	HostSize = 256
)

// utmp structures
// see man utmp
type ExitStatus struct {
	Termination int16
	Exit        int16
}

type TimeVal struct {
	Sec  int32
	Usec int32
}

type Utmp struct {
	record struct {
		Type     int16
		_        [2]byte // padding
		Pid      int32
		Device   [LineSize]byte
		Id       [4]byte
		User     [NameSize]byte
		Host     [HostSize]byte
		Exit     ExitStatus
		Session  int32
		Time     TimeVal
		AddrV6   [16]byte
		Reserved [20]byte
	}
}

func (u *Utmp) Type() int {
	return int(u.record.Type)
}

func (u *Utmp) Pid() int {
	return int(u.record.Pid)
}

func (u *Utmp) Device() string {
	return string(u.record.Device[:getByteLen(u.record.Device[:])])
}

func (u *Utmp) Id() string {
	return string(u.record.Id[:getByteLen(u.record.Id[:])])
}

func (u *Utmp) User() string {
	return string(u.record.User[:getByteLen(u.record.User[:])])
}

func (u *Utmp) Session() int {
	return int(u.record.Session)
}

func (u *Utmp) Addr() net.IP {
	ip := make(net.IP, 16)
	if err := binary.Read(bytes.NewReader(u.record.AddrV6[:]), binary.BigEndian, ip); err != nil {
		return net.IPv4zero
	}
	if bytes.Equal(ip[4:], net.IPv6zero[4:]) {
		// IPv4 address, shorten the slice so that net.IP behaves correctly:
		ip = ip[:4]
	}
	return ip
}

func ReadRecord(r io.Reader) (*Utmp, error) {
	u := new(Utmp)
	if err := binary.Read(r, binary.LittleEndian, &u.record); err != nil {
		return nil, err
	}
	return u, nil
}

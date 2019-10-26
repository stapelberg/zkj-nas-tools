package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/gokrazy/gokrazy"
	"github.com/stapelberg/zkj-nas-tools/internal/wakeonlan"
	"github.com/stapelberg/zkj-nas-tools/ping"
	"github.com/vishvananda/netlink"
	gossh "golang.org/x/crypto/ssh"
)

func logic() error {
	authorizedKeysBytes, err := ioutil.ReadFile("/perm/wolgw/authorized_keys")
	if err != nil {
		return fmt.Errorf("Failed to load authorized_keys, err: %v", err)
	}

	authorized := make(map[string]bool)
	for _, line := range strings.Split(string(authorizedKeysBytes), "\n") {
		if strings.TrimSpace(line) == "" {
			continue // skip empty lines
		}
		if strings.HasPrefix(line, "#") {
			continue // skip comments
		}
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return fmt.Errorf("ParseAuthorizedKey(%v): %v", line, err)
		}

		authorized[string(pubKey.Marshal())] = true
	}

	srv := ssh.Server{
		Addr: "[2a02:168:4a00:6a75::b06e:bf30:504b]:2222", // jump.build.zekjur.net:2222
		LocalPortForwardingCallback: func(ctx ssh.Context, host string, port uint32) bool {
			allow := host == "build" && port == 22
			log.Printf("[%v@%v] jump(%v:%v) = %v", ctx.User(), ctx.RemoteAddr(), host, port, allow)
			return allow
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, sshctx ssh.Context) {
				prefix := fmt.Sprintf("[%v@%v]", sshctx.User(), sshctx.RemoteAddr())
				const buildMAC = "b0:6e:bf:30:50:4b"
				log.Printf("%v waking up %s", prefix, buildMAC)
				// TODO: can we inform the client via
				// https://tools.ietf.org/html/rfc4253#section-11.3 about the
				// upcoming possible delay?
				// If no, (how?) do other bastion servers send messages to clients?
				if err := wakeonlan.SendMagicPacket(&net.UDPAddr{
					// TODO: get first private IP address configured on lan0
					// using gokrazy.PrivateInterfaceAddrs()
					IP: net.ParseIP("10.0.0.1"),
				}, buildMAC); err != nil {
					log.Printf("%v WOL: %v", prefix, err)
					// fail open: the machine might already be running
				}
				// Send ICMP ECHO (ping) packets to detect a reachable target
				// before even dispatching to directTCPIPHandler. This results
				// in a shorter delay when connecting because we will not encur
				// TCP’s long backoff between retries.
				var rtt time.Duration
				tick := time.NewTicker(1 * time.Second)
				defer tick.Stop()
				for {
					ctx, canc := context.WithTimeout(context.Background(), 1*time.Second)
					var err error
					rtt, err = ping.PingUnprivileged(ctx, "build")
					canc()
					if err != nil {
						log.Printf("%v ping(build) = %v", prefix, err)
						<-tick.C // rate limit persistent errors
						continue
					}
					break
				}
				log.Printf("%v build reachable (%v RTT), forwarding traffic", prefix, rtt)
				ssh.DirectTCPIPHandler(srv, conn, newChan, sshctx)
			},
		},
	}
	srv.SetOption(ssh.HostKeyFile("/perm/wolgw/host_key"))
	srv.SetOption(ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
		return authorized[string(key.Marshal())]
	}))
	return srv.ListenAndServe()
}

func configureListenAddress() error {
	link, err := netlink.LinkByName("lan0")
	if err != nil {
		return err
	}

	const buildMAC = "b0:6e:bf:30:50:4b"
	// jump → ju → j (0x6a) u (0x75)
	addr, err := netlink.ParseAddr("2a02:168:4a00:6a75::b06e:bf30:504b/64")
	if err != nil {
		return err
	}

	if err := netlink.AddrReplace(link, addr); err != nil {
		return fmt.Errorf("AddrReplace(%v): %v", addr, err)
	}

	return nil
}

func enableUnprivilegedPing() error {
	return ioutil.WriteFile("/proc/sys/net/ipv4/ping_group_range", []byte("0\t2147483647"), 0600)
}

func mustDropPrivileges() {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "WOLGW_PRIVILEGES_DROPPED=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: 65534, // nobody
			Gid: 65534, // nogroup
		},
	}
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	gokrazy.WaitForClock()

	if os.Getenv("WOLGW_PRIVILEGES_DROPPED") != "1" {
		// parent process
		if err := configureListenAddress(); err != nil {
			log.Fatalf("configuring listen address: %v", err)
		}

		if err := enableUnprivilegedPing(); err != nil {
			log.Fatalf("enabling unprivileged ping: %v", err)
		}

		mustDropPrivileges()
		return
	}

	// child process (without privileges)
	if err := logic(); err != nil {
		log.Fatal(err)
	}
}

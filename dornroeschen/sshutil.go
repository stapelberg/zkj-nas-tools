package main

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rsa"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"

	"github.com/stapelberg/rsyncprom"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

type keychain struct {
	key *rsa.PrivateKey
}

func (k *keychain) Key(i int) (ssh.PublicKey, error) {
	if i != 0 {
		return nil, nil
	}
	return ssh.NewPublicKey(&k.key.PublicKey)
}

func (k *keychain) Sign(i int, rand io.Reader, data []byte) (sig []byte, err error) {
	hashFunc := crypto.SHA1
	h := hashFunc.New()
	h.Write(data)
	digest := h.Sum(nil)
	return rsa.SignPKCS1v15(rand, k.key, hashFunc, digest)
}

// Reads an OpenSSH key and provides it as a ssh.ClientAuth.
func openSshClientAuth(path string) (ssh.AuthMethod, error) {
	privateKey, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	return ssh.PublicKeys(signer), err
}

func newSshConnection(host, keypath string) (*ssh.Client, error) {
	// https://tailscale.com/kb/1015/100.x-addresses/
	_, tailscaleNet, err := net.ParseCIDR("100.64.0.0/10")
	if err != nil {
		return nil, err
	}

	// tailscaled is running with --socks5-server=localhost:1055
	tsProxied, err := proxy.SOCKS5("tcp", "localhost:1055", nil, nil)
	if err != nil {
		return nil, err
	}

	tsdialer := proxy.NewPerHost(proxy.Direct, tsProxied)
	tsdialer.AddNetwork(tailscaleNet)

	clientauth, err := openSshClientAuth(keypath)
	if err != nil {
		return nil, err
	}

	clientConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{clientauth},
		// Sending the backup destination IP address to an attacker is
		// not a threat I’m worried about.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := host + ":22"
	conn, err := tsdialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func newLogWriter(logger *log.Logger) io.Writer {
	r, w := io.Pipe()
	scanner := bufio.NewScanner(r)
	go func() {
		for scanner.Scan() {
			logger.Printf("> %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			log.Print(err)
		}
	}()
	return w
}

func sshCommandFor(logger *log.Logger, session *ssh.Session, host, keypath, command string) (start func(context.Context, []string) (io.Reader, error), wait func() int) {
	exitCode := make(chan int, 1)
	return func(context.Context, []string) (io.Reader, error) {
			logger.Printf("ssh(%s)", host)
			conn, err := newSshConnection(host, keypath)
			if err != nil {
				return nil, err
			}
			session, err = conn.NewSession()
			if err != nil {
				return nil, err
			}
			pr, pw, err := os.Pipe()
			if err != nil {
				return nil, err
			}

			session.Stdout = io.MultiWriter(pw, newLogWriter(logger))
			session.Stderr = newLogWriter(logger)
			logger.Printf("(*ssh.Session).Start(%q)", command)
			if err := session.Start(command); err != nil {
				return nil, err
			}

			go func() {
				defer pw.Close()
				if err := session.Wait(); err != nil {
					logger.Printf("(*ssh.Session).Wait() = %v", err)
					if strings.Contains(err.Error(), "exited with status 24") {
						// rsync exits with status code 24 when a file or directory
						// vanishes between listing and transferring it. this can be
						// expected when doing a full backup while working with
						// docker containers, for example, so treat an exit status
						// code 24 as not-an-error:
						exitCode <- 0
						return
					}
					exitCode <- 1
					return
				}
				exitCode <- 0
			}()

			return pr, nil
		}, func() int {
			return <-exitCode
		}
}

func rsyncSSH(host, keypath, command string) (string, error) {
	logFile, err := os.CreateTemp("", "dornröschen-ssh-*.log")
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.LstdFlags)

	var session *ssh.Session
	defer func() {
		if session != nil {
			session.Close()
		}
	}()
	start, wait := sshCommandFor(logger, session, host, keypath, command)

	ctx := context.Background()
	params := rsyncprom.WrapParams{
		Pushgateway: "https://pushgateway.ts.zekjur.net",
		Instance:    "dr@" + host,
		Job:         "rsync",
	}
	return logFile.Name(), rsyncprom.WrapRsync(ctx, &params, nil, start, wait)
}

func sshCommand(logger *log.Logger, host, keypath, command string) error {
	var session *ssh.Session
	defer func() {
		if session != nil {
			session.Close()
		}
	}()
	start, wait := sshCommandFor(logger, session, host, keypath, command)

	rd, err := start(context.Background(), nil)
	if err != nil {
		return err
	}
	go io.Copy(ioutil.Discard, rd)
	if exitCode := wait(); exitCode != 0 {
		return fmt.Errorf("exit code %d", exitCode)
	}
	return nil
}

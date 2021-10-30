package main

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rsa"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/stapelberg/rsyncprom"
	"golang.org/x/crypto/ssh"
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
	clientauth, err := openSshClientAuth(keypath)
	if err != nil {
		log.Fatal(err)
	}

	clientConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{clientauth},
		// Sending the backup destination IP address to an attacker is
		// not a threat I’m worried about.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", host+":22", clientConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
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

func sshCommand(host, keypath, command string) (string, error) {
	logFile, err := os.CreateTemp("", "dornröschen-ssh-*.log")
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	exitCode := make(chan int, 1)
	var session *ssh.Session
	defer func() {
		if session != nil {
			session.Close()
		}
	}()
	logger := log.New(logFile, "", log.LstdFlags)
	start := func(context.Context, []string) (io.Reader, error) {
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
				exitCode <- 1
				return
			}
			exitCode <- 0
		}()

		return pr, nil
	}
	wait := func() int {
		return <-exitCode
	}
	ctx := context.Background()
	params := rsyncprom.WrapParams{
		Pushgateway: "https://pushgateway.ts.zekjur.net",
		Instance:    "dr@" + host,
		Job:         "rsync",
	}
	return logFile.Name(), rsyncprom.WrapRsync(ctx, &params, nil, start, wait)
}

package main

import (
	"crypto"
	"crypto/rsa"
	"fmt"
	"io"
	"io/ioutil"
	"log"

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
	}

	client, err := ssh.Dial("tcp", host+":22", clientConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func sshCommand(host, keypath, command string) (string, error) {
	conn, err := newSshConnection(host, keypath)
	if err != nil {
		return "", err
	}
	session, err := conn.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	if command == "" {
		return "", session.Start(command)
	}
	f, err := ioutil.TempFile("", "dornröschen-")
	if err != nil {
		return "", err
	}
	defer f.Close()
	session.Stdout = f
	session.Stderr = f
	if err := session.Run(command); err != nil {
		return f.Name(), fmt.Errorf("Could not execute SSH command %q, please see %q", command, f.Name())
	}
	return f.Name(), nil
}

package main

import (
	"code.google.com/p/go.crypto/ssh"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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
func openSshClientAuth(path string) (ssh.ClientAuth, error) {
	privateKey, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(privateKey)
	if block == nil {
		return nil, fmt.Errorf(`No key data found in PEM file "%s"`, path)
	}

	rsakey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	clientKey := &keychain{rsakey}
	return ssh.ClientAuthKeyring(clientKey), nil
}

func newSshConnection(host, keypath string) (*ssh.ClientConn, error) {
	clientauth, err := openSshClientAuth(keypath)
	if err != nil {
		log.Fatal(err)
	}

	clientConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.ClientAuth{clientauth},
	}

	clientconn, err := ssh.Dial("tcp", host+":22", clientConfig)
	if err != nil {
		return nil, err
	}
	return clientconn, nil
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
	output, err := session.CombinedOutput(command)
	if err != nil {
		return "", fmt.Errorf(`Could not execute SSH command "%s":
		%s
		%v`, command, output, err)
	}
	return string(output), nil
}

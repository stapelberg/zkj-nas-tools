// Tiny daemon to serve and revoke files.
// The only access control (intentionally!) for revoking is that you know the file name.
// IP-based access control can be used for reading. Note that IP addresses can
// be spoofed, but finding out which IP address to use requires inside
// knowledge of your network, which is unlikely in this scenario.
//
// Configuration is file-based in /etc/revoke/, e.g.:
//
// /etc/revoke/2001:db8:85a3::1000:8a2e:0370:7334/porn
//   (only 2001:db8:85a3::1000:8a2e:0370:7334 can request the file “porn”)
// /etc/revoke/movies
//   (available to everyone)
//
// See also the comment on servable() for file requirements.
package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"golang.org/x/crypto/acme/autocert"
)

var (
	baseDir = flag.String("base_dir", "/etc/revoke",
		"directory in which to look for files to serve")
	listenAddress = flag.String("listen_address", ":8093",
		"host:port on which to listen for HTTP requests")
	acceptForwarded = flag.Bool("accept_forwarded", false,
		"Accept the HTTP X-Forwarded-For header. Only enable when running behind a HTTP reverse proxy")
	tlsCertPath = flag.String("tls_cert_path",
		"",
		"Path to a .pem file containing the TLS certificate.")
	tlsKeyPath = flag.String("tls_key_path",
		"",
		"Path to a .pem file containing the TLS private key.")
	letsEncryptDomain = flag.String("lets_encrypt_domain", "", "Domain name to request a LetsEncrypt certificate for. Mutually exclusive with tls_cert_path and tls_key_path.")

	fileNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// Returns true only if the given path:
//
// • exists
// • is a file
// • belongs to the user which is running this process
// • has permission 0400
// • is inside a directory with permission 07xx (= can be unlinked)
//
// This ensures that we don’t serve files which cannot be revoked.
func servable(path string) bool {
	fi, ferr := os.Stat(path)
	di, derr := os.Stat(filepath.Dir(path))
	if ferr != nil || derr != nil {
		return false
	}

	// The uid stat field is not portable, hence ugly code.
	uid := fi.Sys().(*syscall.Stat_t).Uid

	return fi.Mode().IsRegular() &&
		uid == uint32(os.Geteuid()) &&
		fi.Mode().Perm() == 0400 &&
		(di.Mode().Perm()&0700) == 0700
}

func accessHandler(w http.ResponseWriter, r *http.Request) {
	// NB: This does not actually trigger a DNS lookup,
	// we parse the ip from r.RemoteAddr, which is [ip]:port.
	addr, err := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	if err != nil {
		http.Error(w, "Internal error resolving address", 500)
		return
	}

	ip := addr.IP.String()
	if *acceptForwarded && r.Header.Get("X-Forwarded-For") != "" {
		ip = r.Header.Get("X-Forwarded-For")
	}

	fileName := r.URL.Path[1:]
	if !fileNameRegexp.MatchString(fileName) {
		http.Error(w, "File not found", 404)
		return
	}

	path := filepath.Join(*baseDir, ip, fileName)
	if !servable(path) {
		path = filepath.Join(*baseDir, fileName)
		if !servable(path) {
			http.Error(w, "File not found", 404)
			return
		}
	}

	http.ServeFile(w, r, path)
}

func revokeHandler(w http.ResponseWriter, r *http.Request) {
	fileName := r.URL.Path[len("/_revoke/"):]
	if !fileNameRegexp.MatchString(fileName) {
		http.Error(w, "File not found", 404)
		return
	}
	filepath.Walk(*baseDir, func(path string, info os.FileInfo, err error) error {
		if filepath.Base(path) == fileName {
			err := os.Remove(path)
			if err != nil {
				w.Write([]byte("Error: " + err.Error() + "\n"))
			}
		}
		return nil
	})
	w.Write([]byte(":-(\n"))
}

func listen() (net.Listener, error) {
	if *letsEncryptDomain != "" {
		if *tlsCertPath != "" || *tlsKeyPath != "" {
			return nil, errors.New("lets_encrypt_domain is mutually exclusive with -tls_cert_path and -tls_key_path.")
		}
		return autocert.NewListener(*letsEncryptDomain), nil
	}

	l, err := net.Listen("tcp", *listenAddress)
	if err != nil || *tlsCertPath == "" || *tlsKeyPath == "" {
		return l, err
	}
	tlsConfig := &tls.Config{
		NextProtos:   []string{"http/1.1"},
		Certificates: make([]tls.Certificate, 1),
	}

	tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(*tlsCertPath, *tlsKeyPath)
	if err != nil {
		l.Close()
		return nil, err
	}

	return tls.NewListener(l, tlsConfig), nil
}

func main() {
	flag.Parse()

	http.HandleFunc("/", accessHandler)
	http.HandleFunc("/_revoke/", revokeHandler)

	listener, err := listen()
	if err != nil {
		log.Fatal(err)
	}
	srv := http.Server{Addr: *listenAddress}
	log.Fatal(srv.Serve(listener))
}

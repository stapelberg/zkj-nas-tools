package main

import (
	"flag"
	"io"
	"log"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/stapelberg/zkj-nas-tools/internal/wake"
)

func syntaxFatal() {
	log.Fatalf("syntax: wake <%s>", strings.Join(slices.Sorted(maps.Keys(wake.Hosts)), "|"))
}

func wake1(target wake.Host) error {
	// We can append .monkey-turtle.ts.net, but DNS resolution should work.
	wakeURL := "http://" + target.Relay + ":8911/wake"
	log.Printf("wakeURL: %s", wakeURL)
	resp, err := http.PostForm(wakeURL, url.Values{
		"machine": []string{target.Name},
	})
	if err != nil {
		return err
	}
	log.Printf("wake resp: %v", resp.Status)
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	log.Printf("body: %s", b)
	return nil
}

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		syntaxFatal()
	}
	target, ok := wake.Hosts[flag.Arg(0)]
	if !ok {
		syntaxFatal()
	}
	if err := wake1(target); err != nil {
		log.Fatal(err)
	}
}

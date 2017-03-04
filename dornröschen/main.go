// +build !gokrazy

package main

import (
	"flag"
	"log"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

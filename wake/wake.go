package main

import (
	"context"
	"log"

	"github.com/stapelberg/zkj-nas-tools/internal/wakecli"
)

func main() {
	if err := wakecli.Execute(context.Background()); err != nil {
		log.Fatal(err)
	}
}

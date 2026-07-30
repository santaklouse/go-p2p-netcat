package main

import (
	"os"

	"github.com/santaklouse/go-p2p-netcat/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

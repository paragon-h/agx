package main

import (
	"context"
	"os"

	"github.com/paragon-h/agx/internal/app"
)

var version = "dev"

func main() {
	runner := app.New(os.Stdout, os.Stderr, version)
	os.Exit(runner.Run(context.Background(), os.Args[1:]))
}

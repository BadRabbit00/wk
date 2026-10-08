package main

import (
	"context"
	"os"

	"wk-cli/internal/wk"
)

var exitProcess = os.Exit

func main() {
	exitProcess(wk.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

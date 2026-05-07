package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/BFGConsult/wellknown-overlay/internal/dockerentrypoint"
)

func main() {
	if err := dockerentrypoint.Run(os.Args, os.Stderr, syscall.Exec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

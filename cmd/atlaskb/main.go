package main

import (
	"os"

	"github.com/tgeorge06/atlaskb/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

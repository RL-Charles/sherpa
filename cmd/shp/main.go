package main

import (
	"os"

	"github.com/RL-Charles/sherpa/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

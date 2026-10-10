// Command sherpa is the long name for shp: the same program, for humans who
// prefer typing the product name. Agents use shp.
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

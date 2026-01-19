package main

import (
	"os"

	"github.com/lupppig/notifyctl/cmd/notifyctl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

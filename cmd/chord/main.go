package main

import (
	"os"

	"github.com/tinmancoding/chord/command"
)

func main() {
	root := command.NewRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

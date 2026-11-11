package main

import (
	"os"

	"github.com/njiwa-tz/mail/internal/cli"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"serve"}
	}
	cli.Run(args)
}

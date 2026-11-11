package main

import (
	"os"

	"github.com/njiwa-tz/mail/internal/cli"
)

func main() {
	cli.Run(os.Args[1:])
}

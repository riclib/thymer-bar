// llog - CLI for thymer-bar / lifelog
package main

import (
	"os"

	"thymer-bar/cmd/llog/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

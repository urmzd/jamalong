// Command jamalong runs the jamalong room server.
package main

import (
	"fmt"
	"os"
)

var version, commit, date string

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("jamalong %s (%s, %s)\n", orDev(version), orDev(commit), orDev(date))
		return
	}
	fmt.Fprintln(os.Stderr, "jamalong: the room server is not implemented yet")
	os.Exit(1)
}

func orDev(s string) string {
	if s == "" {
		return "dev"
	}
	return s
}

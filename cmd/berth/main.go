// Command berth runs ssh and forwards the TCP ports that start listening on
// the remote host to the same ports on localhost.
package main

import (
	"os"

	"github.com/shenyizg/berth/internal/app"
)

// version is set at build time with -ldflags "-X main.version=...".
var version string

func main() {
	os.Exit(app.Main(os.Args[1:], version))
}

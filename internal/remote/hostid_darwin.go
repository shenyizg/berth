package remote

import (
	"strings"
	"syscall"
)

// LocalHostID returns the host id that the watcher script would report on
// this machine: the boot session UUID.
func LocalHostID() string {
	id, err := syscall.Sysctl("kern.bootsessionuuid")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

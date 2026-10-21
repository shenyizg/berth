package remote

import (
	"os"
	"strings"
)

// LocalHostID returns the host id that the watcher script would report on
// this machine: the boot id followed by the network namespace. Containers
// share the boot id with their host but usually not the network namespace.
func LocalHostID() string {
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	ns, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(boot)) + ns
}

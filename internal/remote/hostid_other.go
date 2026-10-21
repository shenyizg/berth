//go:build !linux && !darwin

package remote

// LocalHostID returns "", as there is no host id on this system.
func LocalHostID() string {
	return ""
}

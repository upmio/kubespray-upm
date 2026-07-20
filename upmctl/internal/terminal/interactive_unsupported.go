//go:build !darwin && !linux

package terminal

func isInteractive(uintptr) bool {
	return false
}

//go:build !linux

package anytls

// No-op off Linux: on Windows the kill-on-exit job object already terminates
// the node with the panel, and no other platform is a deployment target.
func killStrayNodeProcesses(_ string) int { return 0 }

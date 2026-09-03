//go:build !windows

package anytls

import "os/exec"

func attachChildLifetime(_ *exec.Cmd) {}

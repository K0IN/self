//go:build unix

package runtime

import (
	"fmt"
	"os/exec"
	"syscall"
)

func terminate(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}

func kill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func describeExit(ee *exec.ExitError) string {
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf("killed by signal %s", ws.Signal())
	}
	return fmt.Sprintf("exited with status %d", ee.ExitCode())
}

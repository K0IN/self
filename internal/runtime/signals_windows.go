//go:build windows

package runtime

import (
	"fmt"
	"os/exec"
)

func setSysProcAttr(cmd *exec.Cmd) {}

// Windows has no SIGTERM; closing stdin is the graceful path.
func terminate(cmd *exec.Cmd) {}

func kill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func describeExit(ee *exec.ExitError) string {
	return fmt.Sprintf("exited with status %d", ee.ExitCode())
}

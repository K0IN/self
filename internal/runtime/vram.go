package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// VRAMUsedMiB returns total used NVIDIA memory in MiB. ok is false when the
// host has no nvidia-smi or the command cannot query the active GPU.
func VRAMUsedMiB(ctx context.Context) (mib int, ok bool) {
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return 0, false
	}

	for _, line := range strings.Split(string(output), "\n") {
		var used int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &used); err != nil {
			continue
		}
		mib += used
		ok = true
	}
	return mib, ok
}

// VRAMUsage returns total used NVIDIA memory as text. It reports n/a when the
// host has no nvidia-smi or the command cannot query the active GPU.
func VRAMUsage(ctx context.Context) string {
	mib, ok := VRAMUsedMiB(ctx)
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%d MiB (NVIDIA GPUs)", mib)
}

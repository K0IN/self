package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// VRAMUsage returns total used NVIDIA memory in MiB. It reports n/a when the
// host has no nvidia-smi or the command cannot query the active GPU.
func VRAMUsage(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return "n/a"
	}

	total := 0
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		var used int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &used); err != nil {
			continue
		}
		total += used
		found = true
	}
	if !found {
		return "n/a"
	}
	return fmt.Sprintf("%d MiB (NVIDIA GPUs)", total)
}

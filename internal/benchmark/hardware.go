package benchmark

import (
	"context"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	rt "ai-server/internal/runtime"
)

// DetectHardware describes this machine: CPU, memory and NVIDIA GPUs. Other
// vendors' GPUs are not detected; the caller can add one by name.
func DetectHardware(ctx context.Context) Hardware {
	return Hardware{
		OS:         runtime.GOOS + "/" + runtime.GOARCH,
		CPU:        cpuModel(ctx),
		CPUThreads: runtime.NumCPU(),
		RAMGiB:     math.Round(ramGiB(ctx)*10) / 10,
		GPUs:       nvidiaGPUs(ctx),
	}
}

// GPUMemoryMiB returns the NVIDIA memory in use, and whether it is known.
func GPUMemoryMiB(ctx context.Context) (int, bool) { return rt.VRAMUsedMiB(ctx) }

// GPUUsed judges whether the model ran on a GPU. Never with --device cpu. On a
// detected NVIDIA GPU it did when loading added at least a quarter of the model
// size in GPU memory; a silent CPU fallback adds almost none. A GPU the user
// named (no driver or memory known) is taken at their word.
func GPUUsed(device string, h Hardware, gpuMiB int, modelBytes int64) bool {
	if device == "cpu" || len(h.GPUs) == 0 {
		return false
	}
	for _, g := range h.GPUs {
		if g.Driver != "" || g.VRAMMiB > 0 {
			return gpuMiB >= int(modelBytes>>20)/4
		}
	}
	return true
}

// RigID names a machine for file names: its GPU when the run used one, else
// its CPU. "NVIDIA GeForce RTX 5090" -> "rtx-5090".
func RigID(h Hardware, gpuUsed bool) string {
	if gpuUsed && len(h.GPUs) > 0 {
		id := Slug(h.GPUs[0].Name)
		if n := len(h.GPUs); n > 1 {
			id += "-x" + strconv.Itoa(n)
		}
		return id
	}
	if s := Slug(h.CPU); s != "" {
		return "cpu-" + s
	}
	return "unknown-rig"
}

var (
	slugNoise    = regexp.MustCompile(`(?i)\((r|tm)\)|@\s*[\d.]+\s*ghz|\b(nvidia|geforce|amd|radeon|intel|apple|graphics|processor|cpu|generation)\b|\b\d+-core\b|\bwith\b.*$`)
	slugNonAlnum = regexp.MustCompile(`[^a-z0-9.]+`)
)

// Slug turns a hardware or model name into lowercase letters, digits, dots and
// dashes, dropping vendor noise.
func Slug(name string) string {
	s := strings.ToLower(slugNoise.ReplaceAllString(name, " "))
	s = strings.Trim(slugNonAlnum.ReplaceAllString(s, "-"), "-.")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-.")
	}
	return s
}

func cpuModel(ctx context.Context) string {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			break
		}
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
				return strings.Join(strings.Fields(v), " ")
			}
		}
	case "darwin":
		if out, err := exec.CommandContext(ctx, "sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return "unknown"
}

func ramGiB(ctx context.Context) float64 {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			break
		}
		for _, line := range strings.Split(string(data), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemTotal:" {
				kib, _ := strconv.ParseFloat(f[1], 64)
				return kib / (1 << 20)
			}
		}
	case "darwin":
		if out, err := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output(); err == nil {
			b, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
			return b / (1 << 30)
		}
	}
	return 0
}

func nvidiaGPUs(ctx context.Context) []GPU {
	// Older drivers lack compute_cap, so fall back to the shorter query.
	for _, query := range []string{"name,memory.total,driver_version,compute_cap", "name,memory.total,driver_version"} {
		out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu="+query, "--format=csv,noheader,nounits").Output()
		if err != nil {
			continue
		}
		return parseNvidiaSMI(string(out))
	}
	return nil
}

func parseNvidiaSMI(out string) []GPU {
	var gpus []GPU
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ",")
		if len(f) < 3 {
			continue
		}
		g := GPU{Name: strings.TrimSpace(f[0]), Driver: strings.TrimSpace(f[2])}
		g.VRAMMiB, _ = strconv.Atoi(strings.TrimSpace(f[1]))
		if len(f) > 3 {
			g.ComputeCapability = strings.TrimSpace(f[3])
		}
		gpus = append(gpus, g)
	}
	return gpus
}

package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"ai-server/internal/errs"
)

// BundleSubdir is the engine directory relative to the release root.
const BundleSubdir = "libexec/ai-server"

// Engine is a located engine executable.
type Engine struct {
	Path string
	// LibDir holds bundled shared libraries ("" if none).
	LibDir string
}

// SearchDirs returns candidate engine directories in priority order:
// the explicit override, then the bundle next to the executable
// (<exe>/libexec/ai-server and <exe>/../libexec/ai-server), then the source
// checkout's bin/libexec/ai-server (development only, see sourceTreeBundle).
// $PATH is never searched so an unrelated binary is never picked up by accident.
func SearchDirs(override string) []string {
	if override != "" {
		return []string{override}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	base := filepath.Dir(exe)
	dirs := []string{
		filepath.Join(base, BundleSubdir),
		filepath.Join(base, "..", BundleSubdir),
	}
	if src := sourceTreeBundle(); src != "" {
		dirs = append(dirs, src)
	}
	return dirs
}

// sourceTreeBundle is <checkout>/bin/libexec/ai-server, taken from this file's
// compile-time path so `go run` (binary in the build cache) finds the engines
// that `just runtime` installs. Release builds use -trimpath, which makes the
// path relative, so this returns "" for them.
func sourceTreeBundle() string {
	_, file, _, ok := goruntime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return ""
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file))) // <root>/internal/runtime/discover.go
	return filepath.Join(root, "bin", BundleSubdir)
}

// Find locates engine name in dirs.
func Find(name string, dirs []string) (Engine, error) {
	file := name
	if goruntime.GOOS == "windows" && !strings.HasSuffix(file, ".exe") {
		file += ".exe"
	}
	for _, d := range dirs {
		p := filepath.Join(d, file)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		if goruntime.GOOS != "windows" && st.Mode()&0o111 == 0 {
			return Engine{}, errs.New(errs.RuntimeNotFound, "engine %s is not executable", p)
		}
		e := Engine{Path: p}
		if lib := filepath.Join(d, "lib"); isDir(lib) {
			e.LibDir = lib
		}
		return e, nil
	}
	return Engine{}, errs.New(errs.RuntimeNotFound,
		"engine %q was not found (looked in: %s). Release builds bundle it under %s next to the self binary; for development pass --runtime-dir or run `just runtime`",
		name, strings.Join(dirs, ", "), BundleSubdir)
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// String is used in logs.
func (e Engine) String() string {
	if e.LibDir == "" {
		return e.Path
	}
	return fmt.Sprintf("%s (libs: %s)", e.Path, e.LibDir)
}

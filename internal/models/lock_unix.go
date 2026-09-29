//go:build unix

package models

import (
	"errors"
	"os"
	"syscall"
)

// errLocked means another process holds the download lock.
var errLocked = errors.New("locked")

// lockFile takes an exclusive, non-blocking advisory lock on path. The lock
// is released automatically if the process dies.
func lockFile(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		os.Remove(path)
	}, nil
}

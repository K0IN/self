//go:build windows

package models

import (
	"errors"
	"os"
)

var errLocked = errors.New("locked")

// lockFile uses exclusive creation as a best-effort lock on Windows.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { f.Close(); os.Remove(path) }, nil
}

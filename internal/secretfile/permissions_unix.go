//go:build !windows

package secretfile

import (
	"errors"
	"os"
)

// CheckPermissions rejects a secret that is accessible by group or other
// users. The caller is responsible for checking that the path is a regular
// file before invoking this function.
func CheckPermissions(_ *os.File, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("permissions must not grant group or other access")
	}
	return nil
}

// Protect restricts a newly created secret to its owner.
func Protect(file *os.File) error {
	return file.Chmod(0o600)
}

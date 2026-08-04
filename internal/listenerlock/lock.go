// Package listenerlock prevents more than one local p2p-nc listener from
// claiming the same logical port.
package listenerlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DirectoryEnvironment overrides the listener lock directory. It is intended
// for isolated installations and tests; normal callers should leave it unset.
const DirectoryEnvironment = "P2P_NETCAT_LISTENER_LOCK_DIR"

var processLocks = struct {
	sync.Mutex
	paths map[string]struct{}
}{paths: make(map[string]struct{})}

// Lock owns the process-local and operating-system lock for one logical port.
type Lock struct {
	file     *os.File
	path     string
	once     sync.Once
	closeErr error
}

// Acquire exclusively claims service for the current process. Lock ownership
// is scoped to the local operating-system user and is independent of whether
// the listener carries byte streams or framed UDP datagrams.
func Acquire(service uint16) (*Lock, error) {
	if service == 0 {
		return nil, errors.New("logical port must be between 1 and 65535")
	}
	directory, err := lockDirectory()
	if err != nil {
		return nil, fmt.Errorf("resolve listener lock directory: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create listener lock directory: %w", err)
	}
	path := filepath.Join(directory, fmt.Sprintf("%d.lock", service))

	processLocks.Lock()
	defer processLocks.Unlock()
	if _, exists := processLocks.paths[path]; exists {
		return nil, inUseError(service)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open listener lock for logical port %d: %w", service, err)
	}
	locked, err := tryLockFile(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock logical port %d: %w", service, err)
	}
	if !locked {
		_ = file.Close()
		return nil, inUseError(service)
	}

	processLocks.paths[path] = struct{}{}
	return &Lock{file: file, path: path}, nil
}

// Close releases the logical port. It is safe to call more than once.
func (lock *Lock) Close() error {
	if lock == nil {
		return nil
	}
	lock.once.Do(func() {
		processLocks.Lock()
		defer processLocks.Unlock()
		lock.closeErr = errors.Join(unlockFile(lock.file), lock.file.Close())
		delete(processLocks.paths, lock.path)
	})
	return lock.closeErr
}

func lockDirectory() (string, error) {
	if directory := os.Getenv(DirectoryEnvironment); directory != "" {
		return filepath.Abs(directory)
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "p2p-netcat", "listeners"), nil
}

func inUseError(service uint16) error {
	return fmt.Errorf("logical port %d already has an active listener", service)
}

//go:build windows

package session

import (
	"context"
	"errors"
)

func PTYServer(context.Context, Stream, bool) error {
	return errors.New("PTY-режим -i пока не поддерживается в Windows-сборке")
}

func PTYClient(context.Context, Stream) error {
	return errors.New("PTY-режим -i пока не поддерживается в Windows-сборке")
}

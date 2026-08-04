//go:build !windows

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/creack/pty"
	ptyframe "github.com/santaklouse/go-p2p-netcat/protocol/pty"
	"golang.org/x/term"
)

func PTYServer(ctx context.Context, stream Stream, verbose bool) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	command := exec.CommandContext(ctx, shell, "-l")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return fmt.Errorf("start PTY: %w", err)
	}
	defer terminal.Close()
	if verbose {
		fmt.Fprintf(os.Stderr, "[p2p-nc] PTY login shell started, pid=%d: %s\n", command.Process.Pid, shell)
	}
	var writer sync.Mutex
	type ptyResult struct {
		err        error
		fromOutput bool
	}
	results := make(chan ptyResult, 2)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			count, readErr := terminal.Read(buffer)
			if count > 0 {
				writer.Lock()
				writeErr := ptyframe.WriteFrame(stream, ptyframe.FrameData, buffer[:count])
				writer.Unlock()
				if writeErr != nil {
					results <- ptyResult{err: writeErr, fromOutput: true}
					return
				}
			}
			if readErr != nil {
				results <- ptyResult{err: readErr, fromOutput: true}
				return
			}
		}
	}()
	go func() {
		for {
			frame, readErr := ptyframe.ReadFrame(stream)
			if readErr != nil {
				results <- ptyResult{err: readErr}
				return
			}
			switch frame.Type {
			case ptyframe.FrameData:
				if _, writeErr := terminal.Write(frame.Data); writeErr != nil {
					results <- ptyResult{err: writeErr}
					return
				}
			case ptyframe.FrameResize:
				columns, rows, resizeErr := ptyframe.DecodeResize(frame.Data)
				if resizeErr != nil {
					results <- ptyResult{err: resizeErr}
					return
				}
				_ = pty.Setsize(terminal, &pty.Winsize{Cols: columns, Rows: rows})
			default:
				results <- ptyResult{err: fmt.Errorf("unknown PTY frame: %d", frame.Type)}
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		_ = command.Process.Kill()
		_ = command.Wait()
		return ctx.Err()
	case result := <-results:
		if result.fromOutput && isPTYEnd(result.err) {
			// Unix PTY masters report EIO, rather than EOF, after the last
			// slave descriptor is closed. This is the normal result of `exit`
			// or Ctrl-D in the login shell. Send a graceful half-close before
			// the surrounding session closes the stream so that the client can
			// consume the final "logout" output and observe EOF.
			closeErr := stream.CloseWrite()
			_ = command.Wait()
			return closeErr
		}
		_ = command.Process.Kill()
		_ = command.Wait()
		if errors.Is(result.err, io.EOF) {
			return nil
		}
		return result.err
	}
}

func isPTYEnd(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO)
}

func PTYClient(ctx context.Context, stream Stream) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("interactive mode -i requires a TTY on stdin")
	}
	previous, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, previous)
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	var writer sync.Mutex
	sendResize := func() {
		columns, rows, sizeErr := term.GetSize(fd)
		if sizeErr == nil {
			writer.Lock()
			defer writer.Unlock()
			_ = ptyframe.WriteFrame(stream, ptyframe.FrameResize, ptyframe.EncodeResize(uint16(columns), uint16(rows)))
		}
	}
	sendResize()
	go func() {
		for range resize {
			sendResize()
		}
	}()

	errorsCh := make(chan error, 2)
	go func() {
		buffer := make([]byte, 32*1024)
		escape := false
		for {
			count, readErr := os.Stdin.Read(buffer)
			if count > 0 {
				output := make([]byte, 0, count)
				for _, value := range buffer[:count] {
					if escape {
						escape = false
						if value == 'q' {
							_ = stream.CloseWrite()
							errorsCh <- nil
							return
						}
						output = append(output, 0x05, value)
					} else if value == 0x05 {
						escape = true
					} else {
						output = append(output, value)
					}
				}
				if len(output) > 0 {
					writer.Lock()
					if err := ptyframe.WriteFrame(stream, ptyframe.FrameData, output); err != nil {
						writer.Unlock()
						errorsCh <- err
						return
					}
					writer.Unlock()
				}
			}
			if readErr != nil {
				errorsCh <- readErr
				return
			}
		}
	}()
	go func() {
		for {
			frame, readErr := ptyframe.ReadFrame(stream)
			if readErr != nil {
				errorsCh <- readErr
				return
			}
			if frame.Type == ptyframe.FrameData {
				if _, writeErr := os.Stdout.Write(frame.Data); writeErr != nil {
					errorsCh <- writeErr
					return
				}
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errorsCh:
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

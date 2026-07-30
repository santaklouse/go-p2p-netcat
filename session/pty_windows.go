//go:build windows

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	hostpty "github.com/aymanbagabas/go-pty"
	ptyframe "github.com/santaklouse/go-p2p-netcat/protocol/pty"
	"golang.org/x/term"
)

func PTYServer(ctx context.Context, stream Stream, verbose bool) error {
	terminal, err := hostpty.New()
	if err != nil {
		return fmt.Errorf("создать Windows ConPTY: %w", err)
	}
	defer terminal.Close()
	if err := terminal.Resize(80, 24); err != nil {
		return fmt.Errorf("изменить размер Windows ConPTY: %w", err)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "powershell.exe"
	}
	command := terminal.CommandContext(ctx, shell)
	if err := command.Start(); err != nil {
		return fmt.Errorf("запустить Windows ConPTY shell: %w", err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[p2p-nc] Windows ConPTY shell запущен, pid=%d: %s\n", command.Process.Pid, shell)
	}

	var writer sync.Mutex
	errorsCh := make(chan error, 3)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			count, readErr := terminal.Read(buffer)
			if count > 0 {
				writer.Lock()
				writeErr := ptyframe.WriteFrame(stream, ptyframe.FrameData, buffer[:count])
				writer.Unlock()
				if writeErr != nil {
					errorsCh <- writeErr
					return
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
			switch frame.Type {
			case ptyframe.FrameData:
				if _, writeErr := terminal.Write(frame.Data); writeErr != nil {
					errorsCh <- writeErr
					return
				}
			case ptyframe.FrameResize:
				columns, rows, resizeErr := ptyframe.DecodeResize(frame.Data)
				if resizeErr != nil {
					errorsCh <- resizeErr
					return
				}
				if resizeErr = terminal.Resize(int(columns), int(rows)); resizeErr != nil {
					errorsCh <- resizeErr
					return
				}
			default:
				errorsCh <- fmt.Errorf("неизвестный PTY frame: %d", frame.Type)
				return
			}
		}
	}()
	go func() { errorsCh <- command.Wait() }()

	select {
	case <-ctx.Done():
		_ = command.Process.Kill()
		return ctx.Err()
	case err := <-errorsCh:
		_ = command.Process.Kill()
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func PTYClient(ctx context.Context, stream Stream) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("интерактивный режим -i требует TTY на stdin")
	}
	previous, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, previous)

	var writer sync.Mutex
	sendResize := func() {
		columns, rows, sizeErr := term.GetSize(fd)
		if sizeErr != nil {
			return
		}
		writer.Lock()
		_ = ptyframe.WriteFrame(stream, ptyframe.FrameResize, ptyframe.EncodeResize(uint16(columns), uint16(rows)))
		writer.Unlock()
	}
	sendResize()
	resizeDone := make(chan struct{})
	defer close(resizeDone)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		lastColumns, lastRows, _ := term.GetSize(fd)
		for {
			select {
			case <-resizeDone:
				return
			case <-ticker.C:
				columns, rows, sizeErr := term.GetSize(fd)
				if sizeErr == nil && (columns != lastColumns || rows != lastRows) {
					lastColumns, lastRows = columns, rows
					sendResize()
				}
			}
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
					writeErr := ptyframe.WriteFrame(stream, ptyframe.FrameData, output)
					writer.Unlock()
					if writeErr != nil {
						errorsCh <- writeErr
						return
					}
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

// Package session connects authenticated libp2p streams to local I/O.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

type Stream interface {
	io.Reader
	io.Writer
	io.Closer
	CloseWrite() error
	Reset() error
}

func Bridge(ctx context.Context, stream Stream, input io.Reader, output io.Writer, quitDelay, inactivity time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	activity := make(chan struct{}, 1)
	reportActivity := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	errorsCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(stream, &activityReader{reader: input, activity: reportActivity})
		if err == nil && quitDelay > 0 {
			timer := time.NewTimer(quitDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
		if closeErr := stream.CloseWrite(); err == nil {
			err = closeErr
		}
		errorsCh <- err
	}()
	go func() {
		_, err := io.Copy(&activityWriter{writer: output, activity: reportActivity}, stream)
		errorsCh <- err
	}()

	var idle *time.Timer
	var idleC <-chan time.Time
	if inactivity > 0 {
		idle = time.NewTimer(inactivity)
		idleC = idle.C
		defer idle.Stop()
	}
	completed := 0
	for completed < 2 {
		select {
		case <-ctx.Done():
			_ = stream.Reset()
			return ctx.Err()
		case <-idleC:
			_ = stream.Reset()
			return fmt.Errorf("таймаут простоя %s", inactivity)
		case <-activity:
			if idle != nil {
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(inactivity)
			}
		case err := <-errorsCh:
			completed++
			if err != nil && !errors.Is(err, io.EOF) {
				_ = stream.Reset()
				return err
			}
		}
	}
	return nil
}

func Exec(ctx context.Context, stream Stream, command string, verbose bool) error {
	child := exec.CommandContext(ctx, shellPath(), shellArgument(), command)
	stdin, err := child.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := child.StderrPipe()
	if err != nil {
		return err
	}
	if err := child.Start(); err != nil {
		return err
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "[p2p-nc] запущена команда, pid=%d: %s\n", child.Process.Pid, command)
	}
	var writer sync.Mutex
	copyOutput := func(source io.Reader) {
		buffer := make([]byte, 32*1024)
		for {
			count, readErr := source.Read(buffer)
			if count > 0 {
				writer.Lock()
				_, _ = stream.Write(buffer[:count])
				writer.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}
	var outputGroup sync.WaitGroup
	go func() {
		_, _ = io.Copy(stdin, stream)
		_ = stdin.Close()
	}()
	outputGroup.Add(2)
	go func() { defer outputGroup.Done(); copyOutput(stdout) }()
	go func() { defer outputGroup.Done(); copyOutput(stderr) }()
	waitErr := child.Wait()
	outputGroup.Wait()
	_ = stdin.Close()
	_ = stream.CloseWrite()
	if verbose {
		fmt.Fprintf(os.Stderr, "[p2p-nc] команда завершилась: %v\n", waitErr)
	}
	return waitErr
}

func shellPath() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	if value := os.Getenv("SHELL"); value != "" {
		return value
	}
	return "/bin/sh"
}

func shellArgument() string {
	if runtime.GOOS == "windows" {
		return "/C"
	}
	return "-c"
}

type activityReader struct {
	reader   io.Reader
	activity func()
}

func (r *activityReader) Read(value []byte) (int, error) {
	count, err := r.reader.Read(value)
	if count > 0 {
		r.activity()
	}
	return count, err
}

type activityWriter struct {
	writer   io.Writer
	activity func()
}

func (w *activityWriter) Write(value []byte) (int, error) {
	count, err := w.writer.Write(value)
	if count > 0 {
		w.activity()
	}
	return count, err
}

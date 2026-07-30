// Package app contains the shared entry point used by the p2p-nc and pnc
// command aliases.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/santaklouse/go-p2p-netcat/internal/cli"
)

const torActive = "P2P_NETCAT_TOR_ACTIVE"

// Run executes the CLI and returns a process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	for _, argument := range args {
		if argument == "-V" {
			fmt.Fprintf(stdout, "p2p-nc version %s\n", cli.Version)
			return 0
		}
	}
	if code, handled, err := runUnderTor(args, stdin, stdout, stderr); handled {
		if err != nil && !cli.QuietRequested(args) {
			fmt.Fprintf(stderr, "[p2p-nc] ошибка: %v\n", err)
		}
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := cli.NewRoot()
	command.SetArgs(args)
	command.SetIn(stdin)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetContext(ctx)
	if err := command.Execute(); err != nil {
		if !cli.QuietRequested(args) {
			fmt.Fprintf(stderr, "[p2p-nc] ошибка: %v\n", err)
		}
		return 1
	}
	return 0
}

func runUnderTor(args []string, stdin io.Reader, stdout, stderr io.Writer) (int, bool, error) {
	requested := cli.TorRequested(args)
	if !requested || os.Getenv(torActive) == "1" {
		return 0, false, nil
	}
	if runtime.GOOS == "windows" {
		return 1, true, errors.New("опция -T требует torsocks и поддерживается только на Linux/macOS")
	}
	for _, value := range args {
		if value == "-h" || value == "--help" || value == "-V" || value == "--version" {
			return 0, false, nil
		}
	}
	host := environmentOr("P2P_NETCAT_TOR_HOST", environmentOr("GSOCKET_SOCKS_IP", "127.0.0.1"))
	port := environmentOr("P2P_NETCAT_TOR_PORT", environmentOr("GSOCKET_SOCKS_PORT", "9050"))
	if net.ParseIP(host) == nil {
		return 1, true, fmt.Errorf("Tor SOCKS host должен быть числовым IP-адресом: %s", host)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return 1, true, fmt.Errorf("некорректный Tor SOCKS port: %s", port)
	}
	executable, err := os.Executable()
	if err != nil {
		return 1, true, err
	}
	commandName := environmentOr("P2P_NETCAT_TORSOCKS_COMMAND", "torsocks")
	torArgs := []string{"-i", "-a", host, "-P", port, executable}
	if cli.QuietRequested(args) {
		torArgs = append([]string{"-q"}, torArgs...)
	}
	torArgs = append(torArgs, args...)
	command := exec.Command(commandName, torArgs...)
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
	command.Env = append(os.Environ(), torActive+"=1")
	err = command.Run()
	if err == nil {
		return 0, true, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), true, nil
	}
	if strings.Contains(err.Error(), "executable file not found") {
		return 1, true, fmt.Errorf("не найден %s; установите Tor и torsocks", commandName)
	}
	return 1, true, err
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

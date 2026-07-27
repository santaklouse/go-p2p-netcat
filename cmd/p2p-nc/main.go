package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/santaklouse/go-p2p-netcat/internal/cli"
)

const torActive = "P2P_NETCAT_TOR_ACTIVE"

func main() {
	for _, argument := range os.Args[1:] {
		if argument == "-V" {
			fmt.Printf("p2p-nc version %s\n", cli.Version)
			return
		}
	}
	if code, handled, err := runUnderTor(os.Args[1:]); handled {
		if err != nil && !cli.QuietRequested(os.Args[1:]) {
			fmt.Fprintf(os.Stderr, "[p2p-nc] ошибка: %v\n", err)
		}
		os.Exit(code)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := cli.NewRoot()
	command.SetContext(ctx)
	if err := command.Execute(); err != nil {
		if !cli.QuietRequested(os.Args[1:]) {
			fmt.Fprintf(os.Stderr, "[p2p-nc] ошибка: %v\n", err)
		}
		os.Exit(1)
	}
}

func runUnderTor(args []string) (int, bool, error) {
	requested := false
	for _, value := range args {
		requested = requested || value == "-T" || value == "--tor"
	}
	if !requested || os.Getenv(torActive) == "1" {
		return 0, false, nil
	}
	for _, value := range args {
		if value == "-h" || value == "--help" || value == "-V" || value == "--version" {
			return 0, false, nil
		}
	}
	host := environmentOr("P2P_NETCAT_TOR_HOST", environmentOr("GSOCKET_SOCKS_IP", "127.0.0.1"))
	port := environmentOr("P2P_NETCAT_TOR_PORT", environmentOr("GSOCKET_SOCKS_PORT", "9050"))
	if _, err := strconv.Atoi(port); err != nil {
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
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
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

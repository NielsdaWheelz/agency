// Package main implements a fake runner binary for daemon integration tests.
// Behavior is controlled by the FAKE_RUNNER_MODE environment variable.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type launchCapture struct {
	Args []string `json:"args"`
	CWD  string   `json:"cwd"`
	Mode string   `json:"mode"`
}

func writeCapture(path, mode string) error {
	if path == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(launchCapture{
		Args: os.Args[1:],
		CWD:  cwd,
		Mode: mode,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func main() {
	mode := os.Getenv("FAKE_RUNNER_MODE")
	if err := writeCapture(os.Getenv("FAKE_RUNNER_CAPTURE_PATH"), mode); err != nil {
		fmt.Fprintln(os.Stderr, "failed to write launch capture:", err)
		os.Exit(2)
	}

	switch mode {
	case "exit-ok":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	case "exit-error":
		fmt.Fprintln(os.Stderr, "error: something went wrong")
		os.Exit(1)

	case "sleep":
		// Block forever, respond to any signal by exiting with code 130 (like SIGINT).
		// Use signal.Notify to prevent Go's deadlock detector from triggering.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		os.Exit(130)

	case "ignore-sigint":
		// Ignore SIGINT so stop escalation must use SIGTERM.
		signal.Ignore(syscall.SIGINT)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		<-sig
		os.Exit(143) // 128 + 15 (SIGTERM)

	case "ignore-sigint-sigterm":
		// Ignore SIGINT and SIGTERM so stop escalation must use SIGKILL.
		signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
		// Block forever — only SIGKILL can stop this.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGUSR1) // arbitrary signal to keep runtime alive
		<-sig

	case "write-then-sleep":
		fmt.Fprintln(os.Stdout, `{"type":"progress","message":"working"}`)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		os.Exit(130)

	case "sleep-short":
		// Sleep briefly then exit cleanly.
		time.Sleep(200 * time.Millisecond)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	default:
		// Unknown mode: exit cleanly.
		os.Exit(0)
	}
}

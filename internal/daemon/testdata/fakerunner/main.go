// Package main implements a fake runner binary for daemon integration tests.
// Behavior is controlled by the FAKE_RUNNER_MODE environment variable.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type launchCapture struct {
	Args           []string `json:"args"`
	CWD            string   `json:"cwd"`
	Mode           string   `json:"mode"`
	FirstStdinLine string   `json:"first_stdin_line,omitempty"`
}

func writeCapture(path, mode, firstStdinLine string) error {
	if path == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(launchCapture{
		Args:           os.Args[1:],
		CWD:            cwd,
		Mode:           mode,
		FirstStdinLine: firstStdinLine,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func readFirstStdinLine(timeout time.Duration) (string, error) {
	type readResult struct {
		line string
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			done <- readResult{err: err}
			return
		}
		done <- readResult{line: strings.TrimRight(line, "\r\n")}
	}()

	select {
	case result := <-done:
		return result.line, result.err
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for stdin line")
	}
}

func main() {
	mode := os.Getenv("FAKE_RUNNER_MODE")
	capturePath := os.Getenv("FAKE_RUNNER_CAPTURE_PATH")
	if mode == "capture-stdin-line" {
		line, err := readFirstStdinLine(3 * time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to read stdin line:", err)
			_ = writeCapture(capturePath, mode, "")
			os.Exit(2)
		}
		if err := writeCapture(capturePath, mode, line); err != nil {
			fmt.Fprintln(os.Stderr, "failed to write launch capture:", err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)
	}
	if err := writeCapture(capturePath, mode, ""); err != nil {
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

	case "sleep-then-exit-ok":
		sleepDuration := 1200 * time.Millisecond
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		time.Sleep(sleepDuration)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	case "codex-thread-sleep-then-exit-ok":
		sleepDuration := 1200 * time.Millisecond
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		threadID := strings.TrimSpace(os.Getenv("FAKE_RUNNER_THREAD_ID"))
		if threadID == "" {
			threadID = "thread_fake_123"
		}
		fmt.Fprintf(os.Stdout, "{\"type\":\"thread.started\",\"thread_id\":%q}\n", threadID)
		time.Sleep(sleepDuration)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	case "cursor-session-sleep-then-exit-ok":
		sleepDuration := 1200 * time.Millisecond
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		sessionID := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SESSION_ID"))
		if sessionID == "" {
			sessionID = "sess_fake_123"
		}
		fmt.Fprintf(os.Stdout, "{\"type\":\"system\",\"subtype\":\"init\",\"cwd\":\"/sandbox\",\"model\":\"cursor-agent-test\",\"session_id\":%q}\n", sessionID)
		time.Sleep(sleepDuration)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	default:
		// Unknown mode: exit cleanly.
		os.Exit(0)
	}
}

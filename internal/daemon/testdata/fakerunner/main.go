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
	Args           []string          `json:"args"`
	CWD            string            `json:"cwd"`
	Mode           string            `json:"mode"`
	FirstStdinLine string            `json:"first_stdin_line,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

func capturedEnv() map[string]string {
	raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_CAPTURE_ENV_KEYS"))
	if raw == "" {
		return nil
	}
	env := map[string]string{}
	for _, key := range strings.Split(raw, ",") {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		env[key] = os.Getenv(key)
	}
	return env
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
		Env:            capturedEnv(),
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

	case "codex-mutate-then-exit-ok":
		sleepDuration := 350 * time.Millisecond
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		threadID := strings.TrimSpace(os.Getenv("FAKE_RUNNER_THREAD_ID"))
		if threadID == "" {
			threadID = "thread_fake_mutation"
		}
		targetFile := strings.TrimSpace(os.Getenv("FAKE_RUNNER_MUTATION_FILE"))
		if targetFile == "" {
			targetFile = "codex-mutated.txt"
		}
		payload := []byte("codex semantic mutation\n")
		if err := os.WriteFile(targetFile, payload, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "failed to write mutation file:", err)
			os.Exit(2)
		}
		command := fmt.Sprintf("printf %q > %s", strings.TrimSpace(string(payload)), targetFile)
		fmt.Fprintf(os.Stdout, "{\"type\":\"thread.started\",\"thread_id\":%q}\n", threadID)
		fmt.Fprintf(os.Stdout, "{\"type\":\"item.started\",\"item\":{\"type\":\"command_execution\",\"command\":%q}}\n", command)
		fmt.Fprintf(os.Stdout, "{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\",\"command\":%q,\"exit_code\":0}}\n", command)
		fmt.Fprintf(os.Stdout, "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"content\":[{\"type\":\"text\",\"text\":%q}]}}\n", "Wrote "+targetFile)
		time.Sleep(sleepDuration)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":2,"output_tokens":2}}`)
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

	case "cursor-mutate-toolcall-then-exit-ok":
		sleepDuration := 350 * time.Millisecond
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		sessionID := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SESSION_ID"))
		if sessionID == "" {
			sessionID = "sess_fake_mutation"
		}
		targetFile := strings.TrimSpace(os.Getenv("FAKE_RUNNER_MUTATION_FILE"))
		if targetFile == "" {
			targetFile = "cursor-mutated.txt"
		}
		payload := []byte("cursor semantic mutation\n")
		if err := os.WriteFile(targetFile, payload, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "failed to write mutation file:", err)
			os.Exit(2)
		}
		command := fmt.Sprintf("printf %q > %s", strings.TrimSpace(string(payload)), targetFile)
		fmt.Fprintf(os.Stdout, "{\"type\":\"system\",\"subtype\":\"init\",\"cwd\":\"/sandbox\",\"model\":\"cursor-agent-test\",\"session_id\":%q}\n", sessionID)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Applying mutation"}]}}`)
		fmt.Fprintf(os.Stdout, "{\"type\":\"tool_call\",\"subtype\":\"started\",\"tool_call\":{\"bashToolCall\":{\"command\":%q}}}\n", command)
		fmt.Fprintf(os.Stdout, "{\"type\":\"tool_call\",\"subtype\":\"completed\",\"tool_call\":{\"bashToolCall\":{\"command\":%q,\"exitCode\":0}}}\n", command)
		time.Sleep(sleepDuration)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	case "claude-session-final-sleep":
		sleepDuration := 4 * time.Second
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		sessionID := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SESSION_ID"))
		if sessionID == "" {
			sessionID = "sess_claude_final_sleep"
		}
		fmt.Fprintf(os.Stdout, "{\"type\":\"system\",\"subtype\":\"init\",\"cwd\":\"/sandbox\",\"model\":\"claude-opus-test\",\"session_id\":%q}\n", sessionID)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"turn complete"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		time.Sleep(sleepDuration)
		os.Exit(0)

	case "claude-session-mutate-then-exit-ok":
		sleepDuration := 350 * time.Millisecond
		if raw := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SLEEP_MS")); raw != "" {
			if ms, err := strconv.Atoi(raw); err == nil && ms >= 0 {
				sleepDuration = time.Duration(ms) * time.Millisecond
			}
		}
		sessionID := strings.TrimSpace(os.Getenv("FAKE_RUNNER_SESSION_ID"))
		if sessionID == "" {
			sessionID = "sess_claude_resume_test"
		}
		targetFile := strings.TrimSpace(os.Getenv("FAKE_RUNNER_MUTATION_FILE"))
		if targetFile == "" {
			targetFile = "claude-followup.txt"
		}
		isResumeTurn := false
		for i := 1; i < len(os.Args); i++ {
			if os.Args[i] == "--resume" || os.Args[i] == "-r" {
				isResumeTurn = true
				break
			}
		}
		toolID := "tool-seed"
		toolResult := "seed complete"
		if isResumeTurn {
			f, err := os.OpenFile(targetFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
			if err != nil {
				fmt.Fprintln(os.Stderr, "failed to open mutation file for append:", err)
				os.Exit(2)
			}
			if _, err := f.WriteString("followup turn\n"); err != nil {
				_ = f.Close()
				fmt.Fprintln(os.Stderr, "failed to append followup mutation:", err)
				os.Exit(2)
			}
			_ = f.Close()
			toolID = "tool-followup"
			toolResult = "followup complete"
		} else {
			if err := os.WriteFile(targetFile, []byte("seed turn\n"), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "failed to write seed mutation file:", err)
				os.Exit(2)
			}
		}
		fmt.Fprintf(os.Stdout, "{\"type\":\"system\",\"subtype\":\"init\",\"cwd\":\"/sandbox\",\"model\":\"claude-opus-test\",\"session_id\":%q}\n", sessionID)
		fmt.Fprintf(os.Stdout, "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"Edit\",\"id\":%q,\"input\":{\"path\":%q}}]}}\n", toolID, targetFile)
		fmt.Fprintf(os.Stdout, "{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":%q,\"content\":%q}]}}\n", toolID, toolResult)
		time.Sleep(sleepDuration)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success"}`)
		os.Exit(0)

	default:
		// Unknown mode: exit cleanly.
		os.Exit(0)
	}
}

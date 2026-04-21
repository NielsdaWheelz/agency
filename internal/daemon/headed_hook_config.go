package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/exec"
	agencyfs "github.com/NielsdaWheelz/agency/internal/fs"
	"github.com/NielsdaWheelz/agency/internal/runners"
)

func (s *Server) installHeadedRunnerHooks(ctx context.Context, repoID, invocationID, runner string, runnerArgs []string, sandboxPath string) error {
	if runner != runners.RunnerClaudeCode && runner != runners.RunnerCodex {
		return nil
	}

	logsDir, err := s.Store.EnsureInvocationLogsDir(repoID, invocationID)
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(logsDir, "headed-hook.sh")
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	script := "#!/bin/sh\nexec " + shellQuoteArg(exe) +
		" internal headed-hook --repo-id " + shellQuoteArg(repoID) +
		" --invocation-id " + shellQuoteArg(invocationID) +
		" --runner " + shellQuoteArg(runner) +
		" --data-dir " + shellQuoteArg(s.Store.DataDir) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	command := shellQuoteArg(scriptPath)
	switch runner {
	case runners.RunnerClaudeCode:
		settingsPath := filepath.Join(sandboxPath, ".claude", "settings.local.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			return err
		}
		skipDangerousModePrompt := false
		for i := 0; i < len(runnerArgs); i++ {
			arg := runnerArgs[i]
			if arg == "--permission-mode" && i+1 < len(runnerArgs) && runnerArgs[i+1] == "bypassPermissions" {
				skipDangerousModePrompt = true
				break
			}
			if arg == "--permission-mode=bypassPermissions" {
				skipDangerousModePrompt = true
				break
			}
		}
		if err := writeClaudeHeadedHookConfig(settingsPath, command, skipDangerousModePrompt); err != nil {
			return err
		}
		if err := s.excludeSandboxFiles(ctx, sandboxPath, []string{".claude/settings.local.json"}); err != nil {
			s.recordInvocationWarning(repoID, invocationID, "headed_hook_git_exclude_failed", err.Error(), map[string]any{
				"path": ".claude/settings.local.json",
			})
		}
	case runners.RunnerCodex:
		hooksPath := filepath.Join(sandboxPath, ".codex", "hooks.json")
		if err := os.MkdirAll(filepath.Dir(hooksPath), 0o700); err != nil {
			return err
		}
		if err := writeCodexHeadedHookConfig(hooksPath, command); err != nil {
			return err
		}
		if err := s.excludeSandboxFiles(ctx, sandboxPath, []string{".codex/hooks.json"}); err != nil {
			s.recordInvocationWarning(repoID, invocationID, "headed_hook_git_exclude_failed", err.Error(), map[string]any{
				"path": ".codex/hooks.json",
			})
		}
	}
	return nil
}

func writeClaudeHeadedHookConfig(path, command string, skipDangerousModePrompt bool) error {
	settings, err := readJSONObject(path)
	if err != nil {
		return err
	}
	settings["disableAllHooks"] = false
	if skipDangerousModePrompt {
		settings["skipDangerousModePermissionPrompt"] = true
	} else {
		delete(settings, "skipDangerousModePermissionPrompt")
	}
	hooks := objectAt(settings, "hooks")
	for _, event := range []string{
		"SessionStart",
		"InstructionsLoaded",
		"PreToolUse",
		"PermissionRequest",
		"PermissionDenied",
		"PostToolUse",
		"PostToolUseFailure",
		"Notification",
		"SubagentStart",
		"SubagentStop",
		"SessionEnd",
		"StopFailure",
		"PreCompact",
		"PostCompact",
		"ConfigChange",
		"Elicitation",
		"ElicitationResult",
	} {
		appendHeadedHook(hooks, event, "*", command)
	}
	for _, event := range []string{
		"UserPromptSubmit",
		"Stop",
		"TeammateIdle",
		"TaskCreated",
		"TaskCompleted",
		"CwdChanged",
		"WorktreeCreate",
		"WorktreeRemove",
	} {
		appendHeadedHook(hooks, event, "", command)
	}
	settings["hooks"] = hooks
	return agencyfs.WriteJSONAtomic(path, settings, 0o600)
}

func writeCodexHeadedHookConfig(path, command string) error {
	settings, err := readJSONObject(path)
	if err != nil {
		return err
	}
	hooks := objectAt(settings, "hooks")
	appendHeadedHook(hooks, "SessionStart", "*", command)
	appendHeadedHook(hooks, "PreToolUse", "*", command)
	appendHeadedHook(hooks, "PostToolUse", "*", command)
	appendHeadedHook(hooks, "UserPromptSubmit", "", command)
	appendHeadedHook(hooks, "Stop", "", command)
	settings["hooks"] = hooks
	return agencyfs.WriteJSONAtomic(path, settings, 0o600)
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

func objectAt(parent map[string]any, key string) map[string]any {
	if obj, ok := parent[key].(map[string]any); ok {
		return obj
	}
	return map[string]any{}
}

func appendHeadedHook(hooks map[string]any, event, matcher, command string) {
	group := map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": command,
			"timeout": 30,
		}},
	}
	if matcher != "" {
		group["matcher"] = matcher
	}
	groups, _ := hooks[event].([]any)
	hooks[event] = append(groups, group)
}

func (s *Server) excludeSandboxFiles(ctx context.Context, sandboxPath string, patterns []string) error {
	result, err := s.Runner.Run(ctx, "git", []string{"-C", sandboxPath, "rev-parse", "--git-path", "info/exclude"}, exec.RunOpts{})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git rev-parse --git-path info/exclude failed: %s", strings.TrimSpace(result.Stderr))
	}

	excludePath := strings.TrimSpace(result.Stdout)
	if excludePath == "" {
		return fmt.Errorf("git returned empty info/exclude path")
	}
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(sandboxPath, excludePath)
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o700); err != nil {
		return err
	}

	data, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := "\n" + string(data)
	var b strings.Builder
	for _, pattern := range patterns {
		if !strings.Contains(existing, "\n"+pattern+"\n") {
			b.WriteString(pattern)
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(b.String())
	return err
}

func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

package cobra

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func realCommandDeps(ctx context.Context) (context.Context, exec.CommandRunner, fs.FS, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, nil, "", errors.Wrap(errors.EInternal, "failed to get cwd", err)
	}

	return ctx, exec.NewRealRunner(), fs.NewRealFS(), cwd, nil
}

func realCommandDepsFromCmd(cmd *cobra.Command) (context.Context, exec.CommandRunner, fs.FS, string, error) {
	if cmd == nil {
		return nil, nil, nil, "", errors.New(errors.EInternal, "command context is required")
	}
	return realCommandDeps(cmd.Context())
}

func rewriteTargetFirstArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	rewritten := append([]string(nil), args...)
	switch rewritten[0] {
	case "repo", "r":
		return rewriteRepoArgs(rewritten)
	case "worktree", "wt":
		return rewriteWorktreeArgs(rewritten)
	case "agent", "ag":
		return rewriteAgentArgs(rewritten)
	default:
		return rewritten
	}
}

func rewriteRepoArgs(args []string) []string {
	if len(args) < 2 || isCLIFlag(args[1]) {
		return args
	}
	switch args[1] {
	case "add", "ls", "help", "_show", "_rm":
		return args
	}
	target := args[1]
	if len(args) == 2 || isCLIFlag(args[2]) {
		return rewriteTargetArgs(args[0], "_show", target, args[2:])
	}
	if args[2] == "rm" {
		return rewriteTargetArgs(args[0], "_rm", target, args[3:])
	}
	return args
}

func rewriteWorktreeArgs(args []string) []string {
	if len(args) < 2 || isCLIFlag(args[1]) {
		return args
	}
	switch args[1] {
	case "create", "ls", "help", "_show", "_path", "_open", "_shell", "_rm", "_rebase", "_pr_sync", "_pr_merge":
		return args
	case "show", "path", "open", "shell", "rm", "pr", "rebase", "merge":
		return args
	}
	target := args[1]
	if len(args) == 2 || isCLIFlag(args[2]) {
		return rewriteTargetArgs(args[0], "_show", target, args[2:])
	}
	switch args[2] {
	case "path":
		return rewriteTargetArgs(args[0], "_path", target, args[3:])
	case "open":
		return rewriteTargetArgs(args[0], "_open", target, args[3:])
	case "shell":
		return rewriteTargetArgs(args[0], "_shell", target, args[3:])
	case "rm":
		return rewriteTargetArgs(args[0], "_rm", target, args[3:])
	case "rebase":
		return rewriteTargetArgs(args[0], "_rebase", target, args[3:])
	case "pr":
		if len(args) < 4 {
			return args
		}
		switch args[3] {
		case "sync":
			return rewriteTargetArgs(args[0], "_pr_sync", target, args[4:])
		case "merge":
			return rewriteTargetArgs(args[0], "_pr_merge", target, args[4:])
		}
	}
	return args
}

func rewriteAgentArgs(args []string) []string {
	if len(args) < 2 || isCLIFlag(args[1]) {
		return args
	}
	switch args[1] {
	case "start", "ls", "help", "_show", "_check", "_diff", "_history", "_history_logs", "_open", "_path", "_shell", "_attach", "_clients", "_stop", "_kill", "_land", "_discard", "_followup", "_recreate", "_restore":
		return args
	case "show", "check", "diff", "history", "open", "path", "shell", "attach", "clients", "stop", "kill", "land", "discard", "followup", "recreate", "restore", "logs", "checkpoint", "restart":
		return args
	}
	target := args[1]
	if len(args) == 2 || isCLIFlag(args[2]) {
		return rewriteTargetArgs(args[0], "_show", target, args[2:])
	}
	switch args[2] {
	case "check":
		return rewriteTargetArgs(args[0], "_check", target, args[3:])
	case "diff":
		return rewriteTargetArgs(args[0], "_diff", target, args[3:])
	case "history":
		if len(args) >= 4 && args[3] == "logs" {
			return rewriteTargetArgs(args[0], "_history_logs", target, args[4:])
		}
		return rewriteTargetArgs(args[0], "_history", target, args[3:])
	case "open":
		return rewriteTargetArgs(args[0], "_open", target, args[3:])
	case "path":
		return rewriteTargetArgs(args[0], "_path", target, args[3:])
	case "shell":
		return rewriteTargetArgs(args[0], "_shell", target, args[3:])
	case "attach":
		return rewriteTargetArgs(args[0], "_attach", target, args[3:])
	case "clients":
		return rewriteTargetArgs(args[0], "_clients", target, args[3:])
	case "stop":
		return rewriteTargetArgs(args[0], "_stop", target, args[3:])
	case "kill":
		return rewriteTargetArgs(args[0], "_kill", target, args[3:])
	case "land":
		return rewriteTargetArgs(args[0], "_land", target, args[3:])
	case "discard":
		return rewriteTargetArgs(args[0], "_discard", target, args[3:])
	case "followup":
		return rewriteTargetArgs(args[0], "_followup", target, args[3:])
	case "recreate":
		return rewriteTargetArgs(args[0], "_recreate", target, args[3:])
	case "restore":
		return rewriteTargetArgs(args[0], "_restore", target, args[3:])
	}
	return args
}

func rewriteTargetArgs(noun, internalVerb, target string, tail []string) []string {
	rewritten := make([]string, 0, 3+len(tail))
	rewritten = append(rewritten, noun, internalVerb, target)
	rewritten = append(rewritten, tail...)
	return rewritten
}

func isCLIFlag(arg string) bool {
	return strings.HasPrefix(arg, "-")
}

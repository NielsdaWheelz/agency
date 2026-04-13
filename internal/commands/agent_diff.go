package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

// AgentDiffOpts holds options for the agent diff command.
type AgentDiffOpts struct {
	InvocationRef   string
	RepoFlag        string
	JSON            bool
	TurnID          string
	TurnRange       string
	DataDirOverride string
}

// AgentDiff shows the diff between sandbox and base_commit.
func AgentDiff(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts AgentDiffOpts, stdout, stderr io.Writer) error {
	if opts.TurnID != "" && opts.TurnRange != "" {
		return errors.New(errors.EUsage, "use either --turn or --turn-range, not both")
	}

	turnStart, turnEnd, err := parseTurnRange(opts.TurnRange)
	if err != nil {
		return err
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoFlag:      opts.RepoFlag,
		AllowAllRepos: false,
		CmdName:       "agent diff",
	})
	if err != nil {
		return err
	}

	result, err := ns.client.GetInvocationDiff(ctx, opts.InvocationRef, repoCtx.RepoID, daemonclient.GetInvocationDiffOpts{
		IncludePatch:       true,
		IncludeUncommitted: true,
		TurnID:             strings.TrimSpace(opts.TurnID),
		TurnStartID:        turnStart,
		TurnEndID:          turnEnd,
	})
	if err != nil {
		return err
	}

	diff := result.Data

	if opts.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(diff)
	}

	if diff.TurnContext != nil {
		_, _ = fmt.Fprintf(stdout, "Turn context:\n")
		switch diff.TurnContext.Selector.Kind {
		case "range":
			_, _ = fmt.Fprintf(stdout, "  selector:      %s..%s\n", diff.TurnContext.Selector.StartTurnID, diff.TurnContext.Selector.EndTurnID)
		default:
			_, _ = fmt.Fprintf(stdout, "  selector:      %s\n", diff.TurnContext.Selector.TurnID)
		}
		_, _ = fmt.Fprintf(stdout, "  checkpoints:   %d -> %d\n", diff.TurnContext.StartCheckpointID, diff.TurnContext.EndCheckpointID)
		_, _ = fmt.Fprintf(stdout, "  commit_range:  %s..%s\n\n", diff.TurnContext.FromCommit, diff.TurnContext.ToCommit)
	}

	_, _ = fmt.Fprintf(stdout, "Commits in sandbox:\n")
	_, _ = fmt.Fprintf(stdout, "==================\n")

	if diff.HasCommits && diff.CommittedRange != nil {
		for _, commit := range diff.CommittedRange.Commits {
			sha := commit.SHA
			if len(sha) > 8 {
				sha = sha[:8]
			}
			_, _ = fmt.Fprintf(stdout, "%s %s\n", sha, commit.Summary)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "(no commits)\n")
	}

	_, _ = fmt.Fprintf(stdout, "\nFile diff (base_commit vs sandbox):\n")
	_, _ = fmt.Fprintf(stdout, "====================================\n")

	if diff.HasCommits && diff.CommittedRange != nil {
		if diff.CommittedRange.Patch != "" {
			_, _ = fmt.Fprint(stdout, diff.CommittedRange.Patch)
		} else {
			_, _ = fmt.Fprintf(stdout, "(diffstat: %s)\n", diff.CommittedRange.Diffstat)
		}
		if diff.CommittedRange.PatchTruncated {
			_, _ = fmt.Fprintf(stderr, "warning: patch was truncated (max bytes: %d)\n", diff.CommittedRange.PatchBytes)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "(no changes)\n")
	}

	if diff.HasUncommitted && diff.WorkingTree != nil {
		_, _ = fmt.Fprintf(stdout, "\nUncommitted changes in sandbox:\n")
		_, _ = fmt.Fprintf(stdout, "================================\n")
		if diff.WorkingTree.Patch != "" {
			_, _ = fmt.Fprint(stdout, diff.WorkingTree.Patch)
		} else {
			_, _ = fmt.Fprintf(stdout, "(diffstat: %s)\n", diff.WorkingTree.Diffstat)
		}
	}

	return nil
}

func parseTurnRange(turnRange string) (string, string, error) {
	trimmed := strings.TrimSpace(turnRange)
	if trimmed == "" {
		return "", "", nil
	}
	if strings.Count(trimmed, "..") != 1 {
		return "", "", errors.NewWithDetails(
			errors.EUsage,
			"invalid --turn-range value",
			map[string]string{
				"hint": "use --turn-range <start_entry_id>..<end_entry_id>",
			},
		)
	}
	start, end, ok := strings.Cut(trimmed, "..")
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if !ok || start == "" || end == "" {
		return "", "", errors.NewWithDetails(
			errors.EUsage,
			"invalid --turn-range value",
			map[string]string{
				"hint": "use --turn-range <start_entry_id>..<end_entry_id>",
			},
		)
	}
	return start, end, nil
}

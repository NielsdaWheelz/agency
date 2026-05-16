package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/render"
)

// writeAgentLSJSONFromDTO outputs invocation list as JSON from daemon DTOs.
func writeAgentLSJSONFromDTO(w io.Writer, invocations []daemon.InvocationDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(invocations)
}

// writeAgentLSHumanFromDTO outputs invocation list in human-readable format from daemon DTOs.
func writeAgentLSHumanFromDTO(w io.Writer, invocations []daemon.InvocationDTO) error {
	if len(invocations) == 0 {
		_, _ = fmt.Fprintln(w, "No agent invocations found.")
		return nil
	}

	for _, inv := range invocations {
		name := ""
		if inv.InvocationName != "" {
			name = " (" + inv.InvocationName + ")"
		}

		attentionStr := ""
		if len(inv.AttentionFlags) > 0 {
			for _, flag := range inv.AttentionFlags {
				attentionStr += " [" + flag + "]"
			}
		}

		_, _ = fmt.Fprintf(w, "%s  %s  %s  %s%s%s\n",
			inv.InvocationID,
			inv.Runner,
			inv.Mode,
			inv.State,
			name,
			attentionStr,
		)

		detailParts := make([]string, 0, 3)
		repo := strings.TrimSpace(inv.RepoID)
		if repo != "" {
			if repoName := strings.TrimSpace(inv.RepoName); repoName != "" {
				detailParts = append(detailParts, "repo: "+repoName+" ("+repo+")")
			} else {
				detailParts = append(detailParts, "repo: "+repo)
			}
		}
		worktree := strings.TrimSpace(inv.WorktreeID)
		if worktree != "" {
			if strings.TrimSpace(inv.WorktreeName) != "" {
				detailParts = append(detailParts, "worktree: "+inv.WorktreeName+" ("+worktree+")")
			} else {
				detailParts = append(detailParts, "worktree: "+worktree)
			}
		}
		if statusSummary := strings.TrimSpace(inv.StatusSummary); statusSummary != "" {
			detailParts = append(detailParts, "summary: "+statusSummary)
		}
		if inv.LatestActivity != nil {
			latestLabel := formatLatestActivityLabel(inv.LatestActivity)
			if latestLabel != "" {
				turnID := strings.TrimSpace(inv.LatestActivity.TurnID)
				if turnID != "" {
					detailParts = append(detailParts, "latest["+turnID+"]: "+latestLabel)
				} else {
					detailParts = append(detailParts, "latest: "+latestLabel)
				}
			}
		}
		if len(detailParts) > 0 {
			_, _ = fmt.Fprintf(w, "    %s\n", strings.Join(detailParts, " | "))
		}
	}

	return nil
}

// writeAgentShowJSONFromDTO outputs invocation details as JSON from daemon DTO.
func writeAgentShowJSONFromDTO(w io.Writer, inv *daemon.InvocationDTO) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}

// writeAgentShowHumanFromDTO outputs invocation details in human-readable format from daemon DTO.
func writeAgentShowHumanFromDTO(w io.Writer, inv *daemon.InvocationDTO) error {
	_, _ = fmt.Fprintf(w, "invocation_id:          %s\n", inv.InvocationID)
	if inv.InvocationName != "" {
		_, _ = fmt.Fprintf(w, "name:                   %s\n", inv.InvocationName)
	}
	worktree := strings.TrimSpace(inv.WorktreeID)
	if strings.TrimSpace(inv.WorktreeName) != "" {
		if worktree != "" {
			_, _ = fmt.Fprintf(w, "worktree:               %s (%s)\n", inv.WorktreeName, worktree)
		} else {
			_, _ = fmt.Fprintf(w, "worktree:               %s\n", inv.WorktreeName)
		}
	} else {
		if worktree == "" {
			worktree = "-"
		}
		_, _ = fmt.Fprintf(w, "worktree:               %s\n", worktree)
	}
	repo := strings.TrimSpace(inv.RepoID)
	if strings.TrimSpace(inv.RepoName) != "" {
		if repo != "" {
			_, _ = fmt.Fprintf(w, "repo:                   %s (%s)\n", inv.RepoName, repo)
		} else {
			_, _ = fmt.Fprintf(w, "repo:                   %s\n", inv.RepoName)
		}
	} else {
		if repo == "" {
			repo = "-"
		}
		_, _ = fmt.Fprintf(w, "repo:                   %s\n", repo)
	}
	_, _ = fmt.Fprintf(w, "runner:                 %s\n", inv.Runner)
	_, _ = fmt.Fprintf(w, "mode:                   %s\n", inv.Mode)
	_, _ = fmt.Fprintf(w, "state:                  %s\n", inv.State)
	if strings.TrimSpace(inv.Reason) != "" {
		_, _ = fmt.Fprintf(w, "reason:                 %s\n", inv.Reason)
	}
	if strings.TrimSpace(inv.StatusSummary) != "" {
		_, _ = fmt.Fprintf(w, "status_summary:         %s\n", inv.StatusSummary)
	}
	if inv.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:         %s\n", inv.LandingStatus)
	}
	if inv.LatestActivity != nil {
		if strings.TrimSpace(inv.LatestActivity.TurnID) != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_turn:   %s\n", inv.LatestActivity.TurnID)
		}
		if strings.TrimSpace(inv.LatestActivity.Kind) != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_kind:   %s\n", inv.LatestActivity.Kind)
		}
		if latestLabel := formatLatestActivityLabel(inv.LatestActivity); latestLabel != "" {
			_, _ = fmt.Fprintf(w, "latest_activity:        %s\n", latestLabel)
		}
		for _, toolLine := range latestActivityToolSummaries(inv.LatestActivity) {
			_, _ = fmt.Fprintf(w, "latest_activity_tool:   %s\n", toolLine)
		}
		if inv.LatestActivity.CheckpointID > 0 {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint: %d\n", inv.LatestActivity.CheckpointID)
		}
		if description := strings.TrimSpace(inv.LatestActivity.CheckpointDescription); description != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_description: %s\n", description)
		}
		if diffstat := strings.TrimSpace(inv.LatestActivity.CheckpointDiffstat); diffstat != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_diffstat: %s\n", diffstat)
		}
		if pathsSummary := latestActivityCheckpointPathSummary(inv.LatestActivity); pathsSummary != "" {
			_, _ = fmt.Fprintf(w, "latest_activity_checkpoint_paths: %s\n", pathsSummary)
		}
	}
	if len(inv.AttentionFlags) > 0 {
		_, _ = fmt.Fprintf(w, "attention_flags:        %v\n", inv.AttentionFlags)
	}
	_, _ = fmt.Fprintf(w, "started_at:             %s\n", inv.StartedAt)
	if inv.FinishedAt != "" {
		_, _ = fmt.Fprintf(w, "finished_at:            %s\n", inv.FinishedAt)
	}
	_, _ = fmt.Fprintf(w, "sandbox_path:           %s\n", inv.SandboxPath)
	if inv.LogsDir != "" {
		_, _ = fmt.Fprintf(w, "logs_dir:               %s\n", inv.LogsDir)
	}
	attachCommand := ""
	if inv.Navigation != nil {
		attachCommand = strings.TrimSpace(inv.Navigation.AttachCommand)
		if strings.TrimSpace(inv.Navigation.HistoryCommand) != "" {
			_, _ = fmt.Fprintf(w, "history_command:        %s\n", inv.Navigation.HistoryCommand)
		}
		if strings.TrimSpace(inv.Navigation.DiffCommand) != "" {
			_, _ = fmt.Fprintf(w, "diff_command:           %s\n", inv.Navigation.DiffCommand)
		}
		if strings.TrimSpace(inv.Navigation.LatestTurnID) != "" {
			_, _ = fmt.Fprintf(w, "latest_turn_id:         %s\n", inv.Navigation.LatestTurnID)
		}
	}
	if attachCommand == "" && strings.TrimSpace(inv.Mode) == "headed" && strings.TrimSpace(inv.InvocationID) != "" && strings.TrimSpace(inv.RepoID) != "" {
		attachCommand = fmt.Sprintf("agency agent %s attach --repo %s", inv.InvocationID, inv.RepoID)
	}
	if attachCommand != "" {
		_, _ = fmt.Fprintf(w, "attach_command:         %s\n", attachCommand)
	}
	return nil
}

func writeAgentCheckHumanFromDTO(w io.Writer, check *daemon.InvocationCheckData) error {
	if check == nil {
		return errors.New(errors.EInternal, "check payload is missing")
	}

	prSyncEligible := "no"
	if check.PRSyncEligible {
		prSyncEligible = "yes"
	}

	_, _ = fmt.Fprintf(w, "state:                %s\n", check.State)
	if strings.TrimSpace(check.Reason) != "" {
		_, _ = fmt.Fprintf(w, "reason:               %s\n", check.Reason)
	}
	_, _ = fmt.Fprintf(w, "pr_sync_eligible:     %s\n", prSyncEligible)
	_, _ = fmt.Fprintf(w, "invocation_id:        %s\n", check.InvocationID)
	_, _ = fmt.Fprintf(w, "repo_id:              %s\n", check.RepoID)
	if check.LandingStatus != "" {
		_, _ = fmt.Fprintf(w, "landing_status:       %s\n", check.LandingStatus)
	}
	if check.RunnerState != "" {
		_, _ = fmt.Fprintf(w, "runner_state:         %s\n", check.RunnerState)
	}
	if strings.TrimSpace(check.RunnerReason) != "" {
		_, _ = fmt.Fprintf(w, "runner_reason:        %s\n", check.RunnerReason)
	}
	if check.RunnerUpdatedAt != "" {
		_, _ = fmt.Fprintf(w, "runner_updated_at:    %s\n", check.RunnerUpdatedAt)
	}
	if check.RunnerSummary != "" {
		_, _ = fmt.Fprintf(w, "runner_summary:       %s\n", check.RunnerSummary)
	}
	if check.HowToTest != "" {
		_, _ = fmt.Fprintf(w, "how_to_test:          %s\n", check.HowToTest)
	}
	attachCommand := strings.TrimSpace(check.Navigation.AttachCommand)
	if attachCommand != "" {
		_, _ = fmt.Fprintf(w, "attach_command:      %s\n", attachCommand)
	}
	_, _ = fmt.Fprintf(w, "\nBlocking reasons:\n")
	if len(check.BlockingReasons) == 0 {
		_, _ = fmt.Fprintf(w, "  (none)\n")
	} else {
		for _, reason := range check.BlockingReasons {
			_, _ = fmt.Fprintf(w, "  - [%s] %s\n", reason.Code, reason.Message)
			if strings.TrimSpace(reason.Hint) != "" {
				_, _ = fmt.Fprintf(w, "      hint: %s\n", reason.Hint)
			}
		}
	}

	_, _ = fmt.Fprintf(w, "\nNavigation:\n")
	_, _ = fmt.Fprintf(w, "  history: %s\n", check.Navigation.HistoryCommand)
	if check.Navigation.DiffCommand != "" {
		_, _ = fmt.Fprintf(w, "  diff:    %s\n", check.Navigation.DiffCommand)
	}
	if check.Navigation.PRSyncCommand != "" {
		_, _ = fmt.Fprintf(w, "  pr_sync: %s\n", check.Navigation.PRSyncCommand)
	}
	if check.Navigation.LatestTurnID != "" {
		_, _ = fmt.Fprintf(w, "  turn:    %s\n", check.Navigation.LatestTurnID)
	}
	return nil
}

func writeAgentHistoryJSONFromDTO(w io.Writer, entries []daemon.TimelineEntryDTO, nextCursor string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Entries    []daemon.TimelineEntryDTO `json:"entries"`
		NextCursor string                    `json:"next_cursor,omitempty"`
	}{
		Entries:    entries,
		NextCursor: nextCursor,
	})
}

func writeAgentHistoryHumanFromTurns(w io.Writer, turns []daemon.Turn, nextCursor string) error {
	if len(turns) == 0 {
		_, _ = fmt.Fprintln(w, "No timeline entries found.")
		return nil
	}
	for _, turn := range turns {
		timestamp := strings.TrimSpace(turn.ShortTimestamp)
		if timestamp == "" {
			timestamp = strings.TrimSpace(turn.Timestamp)
		}
		if timestamp == "" {
			timestamp = "-"
		}

		summary := truncateTimelineText(turn.Summary, 160)
		activity := render.FormatActivityWithExtras(string(turn.Kind), summary, len(turn.ToolCalls), turn.CheckpointID, turn.Restorable)

		_, _ = fmt.Fprintf(w, "%s  %s  %s\n", timestamp, turn.EntryID, activity)
	}

	if nextCursor != "" {
		_, _ = fmt.Fprintf(w, "\nnext_cursor: %s\n", nextCursor)
	}
	return nil
}

func formatLatestActivityLabel(activity *daemon.InvocationLatestActivity) string {
	if activity == nil {
		return ""
	}
	kind := strings.TrimSpace(activity.Kind)
	summary := strings.TrimSpace(activity.Summary)
	toolCount := activity.ToolCallCount
	if toolCount <= 0 {
		toolCount = len(activity.ToolCalls)
	}
	if kind == "" && summary == "" && toolCount == 0 && activity.CheckpointID <= 0 {
		return ""
	}
	return render.FormatActivityWithExtras(
		kind,
		summary,
		toolCount,
		activity.CheckpointID,
		activity.Restorable,
	)
}

func latestActivityToolSummaries(activity *daemon.InvocationLatestActivity) []string {
	if activity == nil || len(activity.ToolCalls) == 0 {
		return nil
	}
	summaries := make([]string, 0, len(activity.ToolCalls))
	for _, tool := range activity.ToolCalls {
		summaries = append(summaries, render.FormatToolCallSummary(
			tool.Name,
			tool.Command,
			tool.HasExit,
			tool.ExitCode,
		))
	}
	return summaries
}

func latestActivityCheckpointPathSummary(activity *daemon.InvocationLatestActivity) string {
	if activity == nil {
		return ""
	}
	return render.FormatChangedPathSummary(
		activity.CheckpointChangedPaths,
		activity.CheckpointChangedCount,
		activity.CheckpointPathsTrimmed,
	)
}

func truncateTimelineText(value string, max int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

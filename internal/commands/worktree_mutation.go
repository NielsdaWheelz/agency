package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

const worktreeMergePollInterval = 500 * time.Millisecond

// WorktreeRmOpts holds options for the worktree rm command.
type WorktreeRmOpts struct {
	WorktreeRef string
	RepoRef     string
	Force       bool
	Yes         bool

	// IsInteractive reports whether stdin/stderr are interactive terminals.
	// If nil, defaults to checking os.Stdin + os.Stderr.
	IsInteractive func() bool

	// ConfirmationIn provides interactive confirmation input.
	// If nil, defaults to os.Stdin.
	ConfirmationIn io.Reader
}

// WorktreeRm removes an integration worktree.
func WorktreeRm(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeRmOpts, stdout, stderr io.Writer) error {
	ns, err := setupDaemonNav(ctx, fsys, "")
	if err != nil {
		return err
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree rm",
	})
	if err != nil {
		return err
	}

	if !opts.Yes {
		isInteractive := opts.IsInteractive
		if isInteractive == nil {
			isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()) }
		}
		if !isInteractive() {
			return errors.NewWithDetails(
				errors.EConfirmationRequired,
				"non-interactive removal requires explicit confirmation",
				map[string]string{"hint": "re-run with --yes"},
			)
		}

		_, _ = fmt.Fprint(stderr, "confirm: type 'rm' to proceed: ")
		confirmationIn := opts.ConfirmationIn
		if confirmationIn == nil {
			confirmationIn = os.Stdin
		}
		line, err := bufio.NewReader(io.LimitReader(confirmationIn, maxConfirmationBytes+1)).ReadString('\n')
		if err != nil && err != io.EOF {
			return errors.Wrap(errors.EInternal, "failed to read worktree remove confirmation input", err)
		}
		if len(line) > maxConfirmationBytes {
			return errors.NewWithDetails(
				errors.EInvalidArgument,
				"confirmation input exceeds maximum length",
				map[string]string{"hint": "type 'rm' exactly"},
			)
		}
		if strings.TrimSpace(line) != "rm" {
			return errors.New(errors.EAborted, "worktree remove confirmation failed; expected 'rm'")
		}
	}

	result, err := ns.client.WorktreeRm(ctx, repoCtx.RepoID, opts.WorktreeRef, opts.Force)
	if err != nil {
		return err
	}
	if !result.OK {
		return errors.NewWithDetails(
			errors.Code(result.ErrorCode),
			result.Message,
			map[string]string{"hint": result.Hint},
		)
	}

	_, _ = fmt.Fprintf(stdout, "Removed integration worktree '%s'\n", opts.WorktreeRef)
	return nil
}

// WorktreePRSyncOpts holds options for the worktree pr sync command.
type WorktreePRSyncOpts struct {
	WorktreeRef     string
	RepoRef         string
	AllowDirty      bool
	ForceWithLease  bool
	JSON            bool
	DataDirOverride string
}

// WorktreePRSync performs worktree-scoped branch push + PR create/update via daemon.
func WorktreePRSync(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePRSyncOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree pr sync",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.WorktreePRSync(ctx, opts.WorktreeRef, repoCtx.RepoID, daemonclient.WorktreePRSyncOpts{
		AllowDirty:     opts.AllowDirty,
		ForceWithLease: opts.ForceWithLease,
	})
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.Branch = resp.Branch
			envelope.PRNumber = resp.PRNumber
			envelope.PRURL = resp.PRURL
			envelope.PRAction = resp.PRAction
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}

	_, _ = fmt.Fprintln(stdout, "PR sync complete")
	_, _ = fmt.Fprintf(stdout, "  worktree_id:     %s\n", resp.IntegrationWorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:          %s\n", resp.Branch)
	_, _ = fmt.Fprintf(stdout, "  pr_action:       %s\n", resp.PRAction)
	_, _ = fmt.Fprintf(stdout, "  pr_url:          %s\n", resp.PRURL)
	return nil
}

// WorktreePRMergeOpts holds options for the worktree pr merge command.
type WorktreePRMergeOpts struct {
	WorktreeRef      string
	RepoRef          string
	Squash           bool
	Merge            bool
	Rebase           bool
	NoDeleteBranch   bool
	Yes              bool
	JSON             bool
	AgencyConfigPath string

	DataDirOverride string
	IsInteractive   func() bool
	ConfirmationIn  io.Reader
}

// WorktreePRMerge performs worktree-scoped verify + merge via daemon.
func WorktreePRMerge(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreePRMergeOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	strategyCount := 0
	strategy := "squash"
	if opts.Squash {
		strategyCount++
		strategy = "squash"
	}
	if opts.Merge {
		strategyCount++
		strategy = "merge"
	}
	if opts.Rebase {
		strategyCount++
		strategy = "rebase"
	}
	if strategyCount > 1 {
		return fail(errors.New(errors.EUsage, "at most one of --squash, --merge, --rebase may be specified"))
	}

	confirmationMode := "yes"
	confirmed := true
	if !opts.Yes {
		isInteractive := opts.IsInteractive
		if isInteractive == nil {
			isInteractive = func() bool { return isTerminal(os.Stdin.Fd()) && isTerminal(os.Stderr.Fd()) }
		}
		if !isInteractive() {
			return fail(errors.NewWithDetails(
				errors.EConfirmationRequired,
				"non-interactive merge requires explicit confirmation",
				map[string]string{"hint": "re-run with --yes"},
			))
		}

		_, _ = fmt.Fprint(stderr, "confirm: type 'merge' to proceed: ")
		confirmationIn := opts.ConfirmationIn
		if confirmationIn == nil {
			confirmationIn = os.Stdin
		}
		line, err := bufio.NewReader(io.LimitReader(confirmationIn, maxConfirmationBytes+1)).ReadString('\n')
		if err != nil && err != io.EOF {
			return fail(errors.Wrap(errors.EInternal, "failed to read worktree merge confirmation input", err))
		}
		if len(line) > maxConfirmationBytes {
			return fail(errors.NewWithDetails(
				errors.EInvalidArgument,
				"confirmation input exceeds maximum length",
				map[string]string{"hint": "type 'merge' exactly"},
			))
		}
		if strings.TrimSpace(line) != "merge" {
			return fail(errors.New(errors.EAborted, "merge confirmation failed; expected 'merge'"))
		}
		confirmationMode = "typed"
		confirmed = true
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree pr merge",
	})
	if err != nil {
		return fail(err)
	}

	agencyConfigPath := opts.AgencyConfigPath
	if agencyConfigPath != "" && !filepath.IsAbs(agencyConfigPath) {
		agencyConfigPath = filepath.Join(cwd, agencyConfigPath)
	}

	resp, err := ns.client.WorktreePRMerge(ctx, opts.WorktreeRef, repoCtx.RepoID, daemonclient.WorktreePRMergeOpts{
		Strategy:         strategy,
		ConfirmationMode: confirmationMode,
		Confirmed:        confirmed,
		NoDeleteBranch:   opts.NoDeleteBranch,
		AgencyConfigPath: agencyConfigPath,
	})
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	merge, err := waitForWorktreeMergeTerminal(ctx, ns.client, opts.WorktreeRef, repoCtx.RepoID, func(update string) {
		if opts.JSON || strings.TrimSpace(update) == "" {
			return
		}
		_, _ = fmt.Fprintln(stderr, update)
	})
	if err != nil {
		return fail(err)
	}

	requestID := resp.RequestID
	if strings.TrimSpace(merge.RequestID) != "" {
		requestID = strings.TrimSpace(merge.RequestID)
	}
	if merge.State == "failed" {
		code := errors.EInternal
		if strings.TrimSpace(merge.ErrorCode) != "" {
			code = errors.Code(strings.TrimSpace(merge.ErrorCode))
		}
		message := strings.TrimSpace(merge.ErrorMessage)
		if message == "" {
			message = "merge failed"
		}
		return fail(errors.NewWithDetails(
			code,
			message,
			map[string]string{
				"hint":       strings.TrimSpace(merge.Hint),
				"request_id": requestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.Branch = merge.Branch
			envelope.PRNumber = merge.PRNumber
			envelope.PRURL = merge.PRURL
			envelope.Strategy = merge.Strategy
			envelope.DeleteBranch = merge.DeleteBranch
			envelope.MergeLogPath = merge.MergeLogPath
			envelope.VerifyLogPath = merge.VerifyLogPath
			envelope.ArchiveLogPath = merge.ArchiveLogPath
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = requestID
		})
	}

	_, _ = fmt.Fprintln(stdout, "worktree pr merge complete")
	_, _ = fmt.Fprintf(stdout, "  worktree_id:     %s\n", resp.IntegrationWorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:          %s\n", merge.Branch)
	_, _ = fmt.Fprintf(stdout, "  strategy:        %s\n", merge.Strategy)
	_, _ = fmt.Fprintf(stdout, "  pr_url:          %s\n", merge.PRURL)
	_, _ = fmt.Fprintf(stdout, "  merge_log:       %s\n", merge.MergeLogPath)
	_, _ = fmt.Fprintf(stdout, "  archive_log:     %s\n", merge.ArchiveLogPath)
	_, _ = fmt.Fprintln(stdout, "  state:           archived")
	return nil
}

func waitForWorktreeMergeTerminal(
	ctx context.Context,
	client *daemonclient.Client,
	worktreeRef string,
	repoID string,
	onUpdate func(string),
) (*daemon.WorktreeMergeDTO, error) {
	lastStage := ""
	lastState := ""
	lastSummary := ""

	for {
		result, err := client.GetWorktreeMerge(ctx, worktreeRef, repoID)
		if err != nil {
			return nil, err
		}
		merge := result.Data

		summary := strings.TrimSpace(merge.StatusSummary)
		if onUpdate != nil && (merge.Stage != lastStage || merge.State != lastState || summary != lastSummary) && summary != "" {
			onUpdate("merge: " + summary)
		}
		lastStage = merge.Stage
		lastState = merge.State
		lastSummary = summary

		switch merge.State {
		case "succeeded", "failed":
			return &merge, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(worktreeMergePollInterval):
		}
	}
}

// WorktreeRebaseOpts holds options for the worktree rebase command.
type WorktreeRebaseOpts struct {
	WorktreeRef     string
	RepoRef         string
	JSON            bool
	DataDirOverride string
}

// WorktreeRebase performs worktree-scoped rebase via daemon.
func WorktreeRebase(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts WorktreeRebaseOpts, stdout, stderr io.Writer) error {
	fail := func(err error) error {
		if err == nil || !opts.JSON {
			return err
		}
		return writeAgentMutationJSONError(stdout, err)
	}

	ns, err := setupDaemonNav(ctx, fsys, opts.DataDirOverride)
	if err != nil {
		return fail(err)
	}

	repoCtx, err := ResolveRepoViaClient(ctx, cr, ns.client, cwd, ResolveRepoContextOpts{
		RepoRef:       opts.RepoRef,
		AllowAllRepos: false,
		CmdName:       "worktree rebase",
	})
	if err != nil {
		return fail(err)
	}

	resp, err := ns.client.WorktreeRebase(ctx, opts.WorktreeRef, repoCtx.RepoID)
	if err != nil {
		return fail(err)
	}
	if !resp.OK {
		return fail(errors.NewWithDetails(
			errors.Code(resp.ErrorCode),
			resp.Message,
			map[string]string{
				"hint":       resp.Hint,
				"request_id": resp.RequestID,
			},
		))
	}

	if opts.JSON {
		return writeAgentMutationJSONSuccess(stdout, func(envelope *agentMutationEnvelope) {
			envelope.RepoID = resp.RepoID
			envelope.IntegrationWorktreeID = resp.IntegrationWorktreeID
			envelope.Branch = resp.Branch
			if resp.APIVersion > 0 {
				envelope.APIVersion = resp.APIVersion
			}
			if resp.BuildVersion != "" {
				envelope.BuildVersion = resp.BuildVersion
			}
			envelope.RequestID = resp.RequestID
		})
	}

	_, _ = fmt.Fprintln(stdout, "worktree rebase complete")
	_, _ = fmt.Fprintf(stdout, "  worktree_id:     %s\n", resp.IntegrationWorktreeID)
	_, _ = fmt.Fprintf(stdout, "  branch:          %s\n", resp.Branch)
	_, _ = fmt.Fprintf(stdout, "  base_branch:   %s\n", resp.BaseBranch)
	return nil
}

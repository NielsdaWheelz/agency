package watch

import (
	"context"
	"net/http"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NielsdaWheelz/agency/internal/daemon"
	"github.com/NielsdaWheelz/agency/internal/daemonclient"
)

func TestReviewPage_FromWorkspaceOpensFullInvocationReview(t *testing.T) {
	t.Parallel()

	requestedTurns := make(chan string, 1)
	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invocations/inv-1/diff":
			requestedTurns <- r.URL.Query().Get("turn")
			assert.Equal(t, "repo-1", r.URL.Query().Get("repo_id"))
			assert.Equal(t, "5242880", r.URL.Query().Get("max_patch_bytes"))
			writeDaemonOK(t, w, daemon.InvocationDiffData{
				BaseCommit:       "abc12345",
				SandboxBranchTip: "def67890",
				HasCommits:       true,
				CommittedRange: &daemon.DiffRange{
					From:     "abc12345",
					To:       "def67890",
					Diffstat: "2 files changed, 8 insertions(+), 2 deletions(-)",
					Patch: "diff --git a/internal/watch/model.go b/internal/watch/model.go\n@@ -1 +1 @@\n-old\n+new\n" +
						"diff --git a/internal/watch/review.go b/internal/watch/review.go\n@@ -0,0 +1,2 @@\n+line one\n+line two\n",
				},
			})
		case "/invocations/inv-1/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{
				InvocationID:   "inv-1",
				RepoID:         "repo-1",
				State:          "succeeded",
				Reason:         "awaiting review",
				LandingStatus:  "pending",
				PRSyncEligible: false,
				RunnerSummary:  "finished cleanly",
				HowToTest:      "go test ./internal/watch",
				BlockingReasons: []daemon.InvocationCheckReason{
					{
						Code:    "landing_pending",
						Message: "invocation changes are not landed into integration yet",
						Hint:    "run land before PR sync",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	m := newModel(context.Background(), client, RunOptions{})
	m.snapshot = Snapshot{
		Repos: []daemon.RepoDTO{
			{RepoID: "repo-1", RepoKey: "github.com/acme/one"},
		},
		Worktrees: []daemon.WorktreeDTO{
			{
				WorktreeID:   "wt-1",
				RepoID:       "repo-1",
				WorktreeName: "feature-auth",
				Merge: &daemon.WorktreeMergeDTO{
					State:         "failed",
					StatusSummary: "merge failed during archive cleanup",
					PRNumber:      77,
					PRURL:         "https://github.com/acme/one/pull/77",
					ErrorMessage:  "archive cleanup failed",
					Hint:          "inspect archive.log and retry merge cleanup",
				},
			},
		},
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID:   "inv-1",
				InvocationName: "review agent",
				RepoID:         "repo-1",
				WorktreeID:     "wt-1",
				Runner:         "codex",
				Mode:           "headless",
				State:          "succeeded",
				LandingStatus:  "pending",
			},
		},
	}
	m.selectedIndex = 0
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.width = 160
	m.height = 40

	next, cmd := m.Update(tea.KeyPressMsg{Text: "d"})
	require.NotNil(t, cmd)

	msg := cmd()
	select {
	case turnID := <-requestedTurns:
		assert.Empty(t, turnID)
	default:
		t.Fatal("expected full invocation diff request")
	}

	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, pageReview, nextModel.page)
	assert.Equal(t, pageWorkspace, nextModel.backPage)
	assert.Empty(t, nextModel.reviewTurnID)
	assert.False(t, nextModel.reviewLoading)
	assert.Empty(t, nextModel.reviewError)
	require.Len(t, nextModel.reviewFiles, 2)

	content := nextModel.View().Content
	assert.Contains(t, content, "Scope:      full invocation diff")
	assert.Contains(t, content, "State:      succeeded (awaiting review)")
	assert.Contains(t, content, "landing pending")
	assert.Contains(t, content, "pr sync not yet")
	assert.Contains(t, content, "merge failed during archive cleanup")
	assert.Contains(t, content, "Before workflow progression:")
	assert.Contains(t, content, "invocation changes are not landed into integration yet")
	assert.Contains(t, content, "internal/watch/model.go")
	assert.Contains(t, content, "internal/watch/review.go")
	assert.Contains(t, content, "go test ./internal/watch")
}

func TestReviewPage_FromHistoryOpensTurnScopedReview(t *testing.T) {
	t.Parallel()

	requestedTurns := make(chan string, 1)
	client := daemonclient.NewClient(startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invocations/inv-1/diff":
			requestedTurns <- r.URL.Query().Get("turn")
			writeDaemonOK(t, w, daemon.InvocationDiffData{
				BaseCommit:       "aaa11111",
				SandboxBranchTip: "bbb22222",
				HasCommits:       true,
				CommittedRange: &daemon.DiffRange{
					From:     "aaa11111",
					To:       "bbb22222",
					Diffstat: "1 file changed, 1 insertion(+)",
					Patch:    "diff --git a/checkpoints/cp2.txt b/checkpoints/cp2.txt\n@@ -0,0 +1 @@\n+checkpoint two\n",
				},
				TurnContext: &daemon.DiffTurnContext{
					Selector: daemon.DiffTurnSelector{
						Kind:   "single",
						TurnID: "e-3",
					},
					StartCheckpointID: 1,
					EndCheckpointID:   2,
					FromCommit:        "aaa11111",
					ToCommit:          "bbb22222",
				},
			})
		case "/invocations/inv-1/check":
			writeDaemonOK(t, w, daemon.InvocationCheckData{
				InvocationID:   "inv-1",
				RepoID:         "repo-1",
				State:          "succeeded",
				Reason:         "checkpoint review",
				LandingStatus:  "pending",
				PRSyncEligible: false,
				BlockingReasons: []daemon.InvocationCheckReason{
					{
						Code:    "checkpoint_pending",
						Message: "turn review must complete before workflow progression",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})))

	m := newModel(context.Background(), client, RunOptions{InitialPage: InitialPageHistory, InvocationID: "inv-1", RepoID: "repo-1"})
	m.page = pageHistory
	m.snapshot = Snapshot{
		Invocations: []daemon.InvocationDTO{
			{
				InvocationID: "inv-1",
				RepoID:       "repo-1",
				WorktreeID:   "wt-1",
				Runner:       "codex",
				Mode:         "headless",
				State:        "succeeded",
			},
		},
	}
	m.selectedInvocationID = "inv-1"
	m.selectedRepoID = "repo-1"
	m.historyTurns = historyTestTurns()
	m.historySelectedIndex = 2
	m.historySelectedEntryID = "e-3"
	m.width = 160
	m.height = 40

	next, cmd := m.Update(tea.KeyPressMsg{Text: "d"})
	require.NotNil(t, cmd)

	msg := cmd()
	select {
	case turnID := <-requestedTurns:
		assert.Equal(t, "e-3", turnID)
	default:
		t.Fatal("expected turn-scoped diff request")
	}

	next, _ = next.(model).Update(msg)
	nextModel := next.(model)

	assert.Equal(t, pageReview, nextModel.page)
	assert.Equal(t, pageHistory, nextModel.backPage)
	assert.Equal(t, "e-3", nextModel.reviewTurnID)
	assert.False(t, nextModel.reviewLoading)
	assert.Empty(t, nextModel.reviewError)
	require.Len(t, nextModel.reviewFiles, 1)
	require.NotNil(t, nextModel.reviewDiff.TurnContext)
	assert.Equal(t, "e-3", nextModel.reviewDiff.TurnContext.Selector.TurnID)

	content := nextModel.View().Content
	assert.Contains(t, content, "Scope:      turn e-3")
	assert.Contains(t, content, "State:      succeeded (checkpoint review)")
	assert.Contains(t, content, "turn review must complete before workflow progression")
	assert.Contains(t, content, "checkpoints/cp2.txt")
}

func TestReviewPage_NavigationMovesSelectionScrollsPatchAndReturns(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{})
	m.page = pageReview
	m.backPage = pageHistory
	m.width = 140
	m.height = 24
	m.reviewFilesFocus = true
	m.reviewFiles = []reviewFile{
		{
			key:     "committed:001:a.go",
			title:   "a.go",
			section: "committed",
			lines:   []string{"diff --git a/a.go b/a.go", "@@ -1 +1 @@", "-old", "+new"},
		},
		{
			key:     "committed:002:b.go",
			title:   "b.go",
			section: "committed",
			lines: []string{
				"diff --git a/b.go b/b.go",
				"@@ -1 +1 @@",
				"-before",
				"+after 1",
				"+after 2",
				"+after 3",
				"+after 4",
				"+after 5",
				"+after 6",
				"+after 7",
				"+after 8",
				"+after 9",
				"+after 10",
				"+after 11",
				"+after 12",
			},
		},
	}
	m.reviewSelectedIndex = 0
	m.reviewSelectedKey = m.reviewFiles[0].key

	next, _ := m.Update(tea.KeyPressMsg{Text: "j"})
	nextModel := next.(model)
	assert.Equal(t, 1, nextModel.reviewSelectedIndex)
	assert.Equal(t, "committed:002:b.go", nextModel.reviewSelectedKey)
	assert.True(t, nextModel.reviewFilesFocus)
	assert.Zero(t, nextModel.reviewScroll)

	next, _ = nextModel.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	nextModel = next.(model)
	assert.False(t, nextModel.reviewFilesFocus)

	next, _ = nextModel.Update(tea.KeyPressMsg{Text: "j"})
	nextModel = next.(model)
	assert.Equal(t, 1, nextModel.reviewScroll)

	next, _ = nextModel.Update(tea.KeyPressMsg{Text: "k"})
	nextModel = next.(model)
	assert.Zero(t, nextModel.reviewScroll)

	next, cmd := nextModel.Update(tea.KeyPressMsg{Text: "b"})
	require.Nil(t, cmd)
	nextModel = next.(model)
	assert.Equal(t, pageHistory, nextModel.page)
}

func TestReviewPage_DoesNotSupportReviewedWorkflowKeys(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), nil, RunOptions{})
	m.page = pageReview
	m.width = 140
	m.height = 24
	m.reviewFilesFocus = true
	m.reviewFiles = []reviewFile{
		{
			key:     "committed:001:a.go",
			title:   "a.go",
			section: "committed",
			lines:   []string{"diff --git a/a.go b/a.go", "@@ -1 +1 @@", "-old", "+new"},
		},
		{
			key:     "committed:002:b.go",
			title:   "b.go",
			section: "committed",
			lines:   []string{"diff --git a/b.go b/b.go", "@@ -1 +1 @@", "-before", "+after"},
		},
	}
	m.reviewSelectedIndex = 0
	m.reviewSelectedKey = m.reviewFiles[0].key

	content := m.View().Content
	assert.NotContains(t, content, "space reviewed")
	assert.NotContains(t, content, "n/N unreviewed")

	next, cmd := m.Update(tea.KeyPressMsg{Text: " "})
	require.Nil(t, cmd)
	nextModel := next.(model)
	assert.Equal(t, 0, nextModel.reviewSelectedIndex)
	assert.Equal(t, m.reviewSelectedKey, nextModel.reviewSelectedKey)
	assert.Zero(t, nextModel.reviewScroll)

	next, cmd = nextModel.Update(tea.KeyPressMsg{Text: "n"})
	require.Nil(t, cmd)
	nextModel = next.(model)
	assert.Equal(t, 0, nextModel.reviewSelectedIndex)
	assert.Equal(t, m.reviewSelectedKey, nextModel.reviewSelectedKey)
	assert.Zero(t, nextModel.reviewScroll)

	next, cmd = nextModel.Update(tea.KeyPressMsg{Text: "N"})
	require.Nil(t, cmd)
	nextModel = next.(model)
	assert.Equal(t, 0, nextModel.reviewSelectedIndex)
	assert.Equal(t, m.reviewSelectedKey, nextModel.reviewSelectedKey)
	assert.Zero(t, nextModel.reviewScroll)
}

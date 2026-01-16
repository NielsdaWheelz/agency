// Package commands implements agency CLI commands.
package commands

import (
	"fmt"

	"github.com/NielsdaWheelz/agency/internal/identity"
)

type ghRepoRef struct {
	NameWithOwner string
	Owner         string
}

func resolveGHRepoRef(originURL string) ghRepoRef {
	owner, repo, ok := identity.ParseGitHubOwnerRepo(originURL)
	if !ok {
		return ghRepoRef{}
	}
	return newGHRepoRef(owner, repo)
}

func newGHRepoRef(owner, repo string) ghRepoRef {
	if owner == "" || repo == "" {
		return ghRepoRef{}
	}
	return ghRepoRef{
		NameWithOwner: fmt.Sprintf("%s/%s", owner, repo),
		Owner:         owner,
	}
}

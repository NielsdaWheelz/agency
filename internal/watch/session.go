package watch

import (
	"context"
	"strings"

	"github.com/NielsdaWheelz/agency/internal/daemon"
)

// InvocationSessionLoader reads headed-session facts for one invocation.
type InvocationSessionLoader func(context.Context, string, string) (InvocationSession, error)

// InvocationSession is the headed-session read shape consumed by watch.
type InvocationSession struct {
	InvocationID      string
	RepoID            string
	Status            string
	TmuxSession       string
	ClientCount       int
	Clients           []InvocationSessionClient
	AttachCommand     string
	RecreateAvailable bool
	Hint              string
}

// InvocationSessionClient is one live tmux client attached to a headed invocation session.
type InvocationSessionClient struct {
	Name     string
	TTY      string
	PID      int
	ReadOnly bool
}

func (s InvocationSession) IsLive() bool {
	return strings.EqualFold(strings.TrimSpace(s.Status), "live")
}

func (s InvocationSession) IsMissing() bool {
	return strings.EqualFold(strings.TrimSpace(s.Status), "missing")
}

func (s InvocationSession) ConnectedClientCount() int {
	if s.ClientCount > 0 {
		return s.ClientCount
	}
	return len(s.Clients)
}

func invocationSessionFromDTO(data daemon.InvocationSessionData) InvocationSession {
	session := InvocationSession{
		InvocationID:      data.InvocationID,
		RepoID:            data.RepoID,
		Status:            data.SessionStatus,
		TmuxSession:       data.TmuxSession,
		ClientCount:       data.ClientCount,
		AttachCommand:     data.AttachCommand,
		RecreateAvailable: data.RecreateAvailable,
		Clients:           make([]InvocationSessionClient, 0, len(data.ConnectedClients)),
	}
	for _, client := range data.ConnectedClients {
		session.Clients = append(session.Clients, InvocationSessionClient{
			Name:     client.Name,
			TTY:      client.TTY,
			PID:      client.PID,
			ReadOnly: client.ReadOnly,
		})
	}
	return session
}

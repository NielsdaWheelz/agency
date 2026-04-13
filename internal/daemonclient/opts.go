package daemonclient

import "github.com/NielsdaWheelz/agency/internal/daemon"

type ControlPlaneStartOpts = daemon.ControlPlaneStartRequest
type ControlPlaneStartHeadedOpts = daemon.ControlPlaneStartHeadedRequest
type SubmitFollowUpPromptOpts = daemon.ControlPlaneFollowUpPromptRequest
type WorktreeCreateOpts = daemon.WorktreeCreateRequest
type RestartFromCheckpointOpts = daemon.RestartFromCheckpointRequest
type WorktreePRSyncOpts = daemon.WorktreePRSyncRequest
type WorktreePRMergeOpts = daemon.WorktreePRMergeRequest

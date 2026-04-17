package daemonclient

import "github.com/NielsdaWheelz/agency/internal/daemon"

type ControlPlaneStartOpts = daemon.ControlPlaneStartRequest
type ControlPlaneStartHeadedOpts = daemon.ControlPlaneStartHeadedRequest
type SubmitFollowUpOpts = daemon.ControlPlaneFollowUpRequest
type WorktreeCreateOpts = daemon.WorktreeCreateRequest
type WorktreePRSyncOpts = daemon.WorktreePRSyncRequest
type WorktreePRMergeOpts = daemon.WorktreePRMergeRequest

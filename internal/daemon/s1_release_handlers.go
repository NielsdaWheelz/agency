package daemon

import (
	"net/http"

	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/releasegates"
)

// handleS1Release dispatches /spec/v2.1/s1/release/* routes.
func (s *Server) handleS1Release(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		requestID := getOrCreateRequestID(r)
		s.writeAPIError(w, http.StatusMethodNotAllowed, requestID, "E_METHOD_NOT_ALLOWED", "method not allowed", "", nil)
		return
	}

	switch r.URL.Path {
	case "/spec/v2.1/s1/release/readiness":
		s.handleS1ReleaseReadiness(w, r)
	case "/spec/v2.1/s1/release/closure-report":
		s.handleS1ClosureReport(w, r)
	case "/spec/v2.1/s1/release/freeze-readiness":
		s.handleS1FreezeReadiness(w, r)
	default:
		requestID := getOrCreateRequestID(r)
		s.writeAPIError(w, http.StatusNotFound, requestID, "E_NOT_FOUND", "not found", "", nil)
	}
}

func (s *Server) handleS1ReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	repoRoot, err := s.resolveRepoRootFromQuery(r, requestID, w)
	if err != nil {
		return
	}

	source := releasegates.NewSource(repoRoot)
	svc := releasegates.NewService(source)

	_, svcErr := svc.EvaluateReleaseReadiness(releasegates.ReleaseReadinessRequest{Slice: "S1"}, repoRoot)
	if svcErr != nil {
		code := errors.GetCode(svcErr)
		if code == errors.EGateBlocked {
			ae, _ := errors.AsAgencyError(svcErr)
			s.writeAPIError(w, http.StatusConflict, requestID, string(code), ae.Msg, "", ae.Details)
			return
		}
		if code == errors.EGateSetInvalid {
			ae, _ := errors.AsAgencyError(svcErr)
			s.writeAPIError(w, http.StatusBadRequest, requestID, string(code), ae.Msg, "", ae.Details)
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", svcErr.Error(), "", nil)
		return
	}

	result, _ := svc.EvaluateReleaseReadiness(releasegates.ReleaseReadinessRequest{Slice: "S1"}, repoRoot)

	data := S1ReleaseReadinessData{
		Slice:      result.Slice,
		SliceReady: result.SliceReady,
		GateA:      toS1GateStatusData(result.GateA),
		GateB:      toS1GateStatusData(result.GateB),
	}
	s.writeAPIResponse(w, requestID, data)
}

func (s *Server) handleS1ClosureReport(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	repoRoot, err := s.resolveRepoRootFromQuery(r, requestID, w)
	if err != nil {
		return
	}

	source := releasegates.NewSource(repoRoot)
	svc := releasegates.NewService(source)

	result, svcErr := svc.BuildClosureReport(releasegates.ClosureReportRequest{Slice: "S1"}, repoRoot)
	if svcErr != nil {
		code := errors.GetCode(svcErr)
		if code == errors.EGateSetInvalid {
			ae, _ := errors.AsAgencyError(svcErr)
			s.writeAPIError(w, http.StatusBadRequest, requestID, string(code), ae.Msg, "", ae.Details)
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", svcErr.Error(), "", nil)
		return
	}

	data := S1ClosureReportData{
		Slice: result.Slice,
		GateA: toS1GateClosureData(result.GateA),
		GateB: toS1GateClosureData(result.GateB),
	}
	s.writeAPIResponse(w, requestID, data)
}

func (s *Server) handleS1FreezeReadiness(w http.ResponseWriter, r *http.Request) {
	requestID := getOrCreateRequestID(r)

	repoRoot, err := s.resolveRepoRootFromQuery(r, requestID, w)
	if err != nil {
		return
	}

	source := releasegates.NewSource(repoRoot)
	svc := releasegates.NewService(source)

	result, svcErr := svc.EvaluateFreezeReadiness(releasegates.FreezeReadinessRequest{}, repoRoot)
	if svcErr != nil {
		code := errors.GetCode(svcErr)
		if code == errors.EGateBlocked {
			ae, _ := errors.AsAgencyError(svcErr)
			s.writeAPIError(w, http.StatusConflict, requestID, string(code), ae.Msg, "", ae.Details)
			return
		}
		if code == errors.EGateSetInvalid {
			ae, _ := errors.AsAgencyError(svcErr)
			s.writeAPIError(w, http.StatusBadRequest, requestID, string(code), ae.Msg, "", ae.Details)
			return
		}
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", svcErr.Error(), "", nil)
		return
	}

	data := S1FreezeReadinessData{
		FreezeReady:     result.FreezeReady,
		UnresolvedCount: result.UnresolvedCount,
		SpecPath:        result.SpecPath,
		FirstQuestion:   result.FirstQuestion,
	}
	s.writeAPIResponse(w, requestID, data)
}

// resolveRepoRootFromQuery extracts repo_id from query params and resolves the canonical repo root.
// Writes an error response and returns a non-nil error if resolution fails.
func (s *Server) resolveRepoRootFromQuery(r *http.Request, requestID string, w http.ResponseWriter) (string, error) {
	repoID := r.URL.Query().Get("repo_id")
	if repoID == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, "E_INVALID_REQUEST",
			"repo_id query parameter is required",
			"pass ?repo_id=<repo_id>", nil)
		return "", errors.New(errors.EUsage, "repo_id required")
	}

	repoRecord, exists, err := s.Store.LoadRepoRecord(repoID)
	if err != nil {
		s.writeAPIError(w, http.StatusInternalServerError, requestID, "E_INTERNAL", err.Error(), "", nil)
		return "", err
	}
	if !exists {
		s.writeAPIError(w, http.StatusNotFound, requestID, string(errors.ERepoNotFound),
			"repo not found: "+repoID,
			"run 'agency repo ls' to see registered repos",
			nil)
		return "", errors.New(errors.ERepoNotFound, "repo not found")
	}

	root := repoRecord.PreferredRoot
	if root == "" {
		s.writeAPIError(w, http.StatusBadRequest, requestID, string(errors.ERepoNoAccessibleRoots),
			"repo has no preferred root",
			"re-register the repo with 'agency repo add'",
			nil)
		return "", errors.New(errors.ERepoNoAccessibleRoots, "no root")
	}

	return root, nil
}

func toS1GateStatusData(gs *releasegates.GateStatus) *S1GateStatusData {
	if gs == nil {
		return nil
	}
	blocking := gs.BlockingItems
	if blocking == nil {
		blocking = []string{}
	}
	return &S1GateStatusData{
		GateID:        gs.GateID,
		Status:        gs.Status,
		TotalItems:    gs.TotalItems,
		ClosedItems:   gs.ClosedItems,
		BlockingItems: blocking,
	}
}

func toS1GateClosureData(snap *releasegates.GateClosureSnapshot) *S1GateClosureData {
	if snap == nil {
		return nil
	}
	blocking := snap.BlockingItems
	if blocking == nil {
		blocking = []string{}
	}
	evidence := make([]S1ClosedItemEvidence, len(snap.ClosedEvidence))
	for i, ev := range snap.ClosedEvidence {
		targeted := make([]S1TestEvidenceData, len(ev.TargetedTests))
		for j, te := range ev.TargetedTests {
			targeted[j] = toS1TestEvidenceData(te)
		}
		suite := make([]S1TestEvidenceData, len(ev.SuiteTests))
		for j, te := range ev.SuiteTests {
			suite[j] = toS1TestEvidenceData(te)
		}
		refs := ev.ImplementedRefs
		if refs == nil {
			refs = []string{}
		}
		evidence[i] = S1ClosedItemEvidence{
			IssuePath:       ev.IssuePath,
			ImplementedRefs: refs,
			TargetedTests:   targeted,
			SuiteTests:      suite,
		}
	}
	return &S1GateClosureData{
		GateID:         snap.GateID,
		Status:         snap.Status,
		TotalItems:     snap.TotalItems,
		ClosedItems:    snap.ClosedItems,
		BlockingItems:  blocking,
		ClosedEvidence: evidence,
	}
}

func toS1TestEvidenceData(te releasegates.TestEvidence) S1TestEvidenceData {
	return S1TestEvidenceData{
		IssuePath:   te.IssuePath,
		Command:     te.Command,
		Scope:       te.Scope,
		Result:      te.Result,
		ArtifactRef: te.ArtifactRef,
		RecordedAt:  te.RecordedAt,
	}
}

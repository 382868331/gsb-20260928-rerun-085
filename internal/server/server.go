package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/route"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/store"
	"dependency-workbench/internal/verifier"
)

type Server struct {
	store  *store.Store
	routes *store.RouteStore
	static fs.FS
	mux    *http.ServeMux
	// confirmMu serializes check-outcome submissions so two pages posting
	// records for the same route cannot interleave read-modify-write.
	confirmMu sync.Mutex
}

func New(planStore *store.Store, routeStore *store.RouteStore, static fs.FS) *Server {
	s := &Server{store: planStore, routes: routeStore, static: static, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/scenarios", s.handleScenarios)
	s.mux.HandleFunc("/api/sample", s.handleSample)
	s.mux.HandleFunc("/api/solve", s.handleSolve)
	s.mux.HandleFunc("/api/route/plan", s.handleRoutePlan)
	s.mux.HandleFunc("/api/routes", s.handleRoutes)
	s.mux.HandleFunc("/api/routes/confirm", s.handleRouteConfirm)
	s.mux.HandleFunc("/api/plans/verify", s.handleVerify)
	s.mux.HandleFunc("/api/plans", s.handlePlans)
	s.mux.HandleFunc("/", s.handleStatic)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, scenarios())
}

func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	scenario, ok := scenarioByID(r.URL.Query().Get("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown scenario")
		return
	}
	writeJSON(w, http.StatusOK, scenario)
}

func (s *Server) handleSolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req domain.SolveRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, solver.Solve(req))
}

func (s *Server) handleRoutePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req domain.RouteRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, route.Plan(req))
}

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if s.routes == nil {
		writeError(w, http.StatusServiceUnavailable, "route store is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		routes, err := s.routes.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, routes)
	case http.MethodPost:
		var req domain.RouteRequest
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		fingerprint := fingerprintOf(req)
		if existing, found, err := s.routes.FindByFingerprint(fingerprint); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		} else if found {
			// Saving the same route again (e.g. after an interrupted save)
			// returns the original record instead of duplicating one-time
			// transformations.
			writeJSON(w, http.StatusOK, existing)
			return
		}
		report := route.Plan(req)
		saved := domain.SavedRoute{
			ID:          newID(),
			Fingerprint: fingerprint,
			Request:     req,
			Report:      report,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		syncRouteLedger(&saved)
		if err := s.routes.Save(saved); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
	}
}

// handleRouteConfirm records the user's check outcomes for one step of a
// saved route. Re-sending the identical record is a no-op; a different
// record for an already-recorded step is rejected because executed
// transformations and their verification records cannot be overwritten by
// a new route. The one exception is 资料补录: checks still marked
// insufficient may be filled in later, while final outcomes (success or
// failed) are append-only and any contradiction is rejected.
func (s *Server) handleRouteConfirm(w http.ResponseWriter, r *http.Request) {
	if s.routes == nil {
		writeError(w, http.StatusServiceUnavailable, "route store is not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var payload domain.ConfirmRequest
	if err := decode(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Two pages may submit records for the same route at the same time;
	// serialize the whole read-modify-write so retries stay idempotent and
	// contradictions are rejected instead of silently overwriting history.
	s.confirmMu.Lock()
	defer s.confirmMu.Unlock()
	routes, err := s.routes.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var saved *domain.SavedRoute
	for i := range routes {
		if routes[i].ID == payload.RouteID {
			saved = &routes[i]
		}
	}
	if saved == nil {
		writeError(w, http.StatusNotFound, "unknown route")
		return
	}
	step := findStep(saved, payload.StepIndex)
	if step == nil {
		writeError(w, http.StatusNotFound, "unknown step")
		return
	}
	if recorded(step.Checks) {
		merged, status := mergeRecordedChecks(step.Checks, payload.Checks)
		switch status {
		case "replay":
			// Idempotent retry after an interrupted save: nothing is
			// executed again, the stored record is returned as-is.
			writeJSON(w, http.StatusOK, *saved)
			return
		case "invalid":
			writeError(w, http.StatusBadRequest, "check status must be success, failed or insufficient, and every recorded check must be present")
			return
		case "conflict":
			writeError(w, http.StatusConflict, "该步骤的核对记录已存在，不能被新记录覆盖")
			return
		}
		// 资料补录: only checks that were still insufficient changed.
		step.Checks = merged
	} else {
		for _, check := range payload.Checks {
			switch check.Status {
			case "success", "failed", "insufficient":
			default:
				writeError(w, http.StatusBadRequest, "check status must be success, failed or insufficient")
				return
			}
		}
		if len(payload.Checks) != len(step.Checks) {
			writeError(w, http.StatusBadRequest, "must record an outcome for every attached check")
			return
		}
		step.Checks = append([]domain.StepCheck{}, payload.Checks...)
	}
	allSuccess := true
	for _, check := range step.Checks {
		if check.Status != "success" {
			allSuccess = false
		}
	}
	step.Confirmed = true
	step.Effective = allSuccess
	if !alreadyConfirmed(saved, payload.StepIndex) {
		// Move the step out of the planned list into the executed history,
		// keeping its check record verbatim for any later route.
		remaining := []domain.RouteStep{}
		for _, item := range saved.Report.Steps {
			if item.Index != payload.StepIndex {
				remaining = append(remaining, item)
			}
		}
		saved.Report.Steps = remaining
		saved.Report.ConfirmedSteps = append(saved.Report.ConfirmedSteps, *step)
		saved.Request.Confirmed = append(saved.Request.Confirmed, *step)
	}
	syncRouteLedger(saved)
	if err := s.routes.Update(*saved); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, *saved)
}

// alreadyConfirmed reports whether the step has previously been moved into
// the executed history (e.g. a 资料补录 submission for a held step).
func alreadyConfirmed(saved *domain.SavedRoute, index int) bool {
	for _, step := range saved.Report.ConfirmedSteps {
		if step.Index == index {
			return true
		}
	}
	return false
}

// mergeRecordedChecks compares a new submission with the stored record of
// an already-recorded step. It returns the merged checks and one of:
//
//	replay    - identical record, nothing to do
//	supplement - only insufficient checks were filled in; merged is returned
//	conflict  - a final outcome (success/failed) would be overwritten
//	invalid   - malformed submission
func mergeRecordedChecks(existing []domain.StepCheck, incoming []domain.StepCheck) ([]domain.StepCheck, string) {
	if len(incoming) != len(existing) {
		return nil, "invalid"
	}
	byID := map[string]domain.StepCheck{}
	for _, check := range incoming {
		switch check.Status {
		case "success", "failed", "insufficient":
		default:
			return nil, "invalid"
		}
		byID[check.ID] = check
	}
	merged := make([]domain.StepCheck, 0, len(existing))
	changed := false
	for _, stored := range existing {
		next, ok := byID[stored.ID]
		if !ok {
			return nil, "invalid"
		}
		switch stored.Status {
		case "success", "failed":
			// Final outcomes are append-only: a contradicting record is
			// rejected outright, never merged over history.
			if next.Status != stored.Status {
				return nil, "conflict"
			}
			merged = append(merged, stored)
		default:
			// 资料不足 (or still pending) checks may be supplemented later.
			if next.Status != stored.Status {
				changed = true
			}
			updated := stored
			updated.Status = next.Status
			if next.Note != "" {
				updated.Note = next.Note
			}
			merged = append(merged, updated)
		}
	}
	if !changed {
		return nil, "replay"
	}
	return merged, "supplement"
}

// syncRouteLedger keeps the separately stored ledger of a saved route in
// step with its report: the versions actually executed, every recorded
// check conclusion, and the one-time impacts no rollback can undo.
func syncRouteLedger(saved *domain.SavedRoute) {
	executed := map[string]string{}
	for name, version := range saved.Request.Current {
		executed[name] = version
	}
	conclusions := []domain.CheckRecord{}
	irreversible := []string{}
	steps := append([]domain.RouteStep{}, saved.Report.ConfirmedSteps...)
	// Held steps were executed too: their conclusions and one-time impacts
	// belong in the ledger even though planning does not count them as done.
	steps = append(steps, saved.Report.HeldSteps...)
	sortSteps(steps)
	for _, step := range steps {
		for _, check := range step.Checks {
			if check.Status == "" || check.Status == "pending" {
				continue
			}
			conclusions = append(conclusions, domain.CheckRecord{StepIndex: step.Index, Check: check})
		}
		// Held steps count too: a failed check does not undo a data
		// transformation that was already executed.
		irreversible = append(irreversible, step.Rollback.Irreversible...)
		if !step.Effective {
			continue
		}
		for _, change := range step.Changes {
			if change.To == "" {
				delete(executed, change.Component)
			} else {
				executed[change.Component] = change.To
			}
		}
	}
	saved.Executed = executed
	saved.Conclusions = conclusions
	saved.Irreversible = irreversible
}

func sortSteps(steps []domain.RouteStep) {
	for i := 1; i < len(steps); i++ {
		for j := i; j > 0 && steps[j-1].Index > steps[j].Index; j-- {
			steps[j-1], steps[j] = steps[j], steps[j-1]
		}
	}
}

func findStep(saved *domain.SavedRoute, index int) *domain.RouteStep {
	for i := range saved.Report.ConfirmedSteps {
		if saved.Report.ConfirmedSteps[i].Index == index {
			return &saved.Report.ConfirmedSteps[i]
		}
	}
	for i := range saved.Report.Steps {
		if saved.Report.Steps[i].Index == index {
			return &saved.Report.Steps[i]
		}
	}
	return nil
}

func recorded(checks []domain.StepCheck) bool {
	for _, check := range checks {
		if check.Status != "pending" && check.Status != "" {
			return true
		}
	}
	return false
}

func fingerprintOf(req domain.RouteRequest) string {
	encoded, _ := json.Marshal(req)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

type verifyRequest struct {
	Manifest       domain.Manifest        `json:"manifest"`
	Selection      map[string]string      `json:"selection"`
	Target         string                 `json:"target,omitempty"`
	DesiredVersion string                 `json:"desiredVersion,omitempty"`
	Goals          []domain.MigrationGoal `json:"goals,omitempty"`
	Confirmed      map[string]string      `json:"confirmed,omitempty"`
	Current        map[string]string      `json:"current,omitempty"`
	Locks          map[string]string      `json:"locks,omitempty"`
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var payload verifyRequest
	if err := decode(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req := domain.SolveRequest{
		Manifest:       payload.Manifest,
		Current:        payload.Current,
		Target:         payload.Target,
		DesiredVersion: payload.DesiredVersion,
		Locks:          payload.Locks,
		Goals:          payload.Goals,
		Confirmed:      payload.Confirmed,
	}
	writeJSON(w, http.StatusOK, verifier.VerifyRequest(req, payload.Selection))
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		plans, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range plans {
			verification := verifier.VerifyRequest(plans[i].Request, plans[i].Report.Selection)
			plans[i].Verification = verification
			plans[i].Consistent = consistent(plans[i].Report, verification)
		}
		writeJSON(w, http.StatusOK, plans)
	case http.MethodPost:
		var req domain.SolveRequest
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		report := solver.Solve(req)
		// Independent re-check against the exact manifest snapshot carried in
		// the request. The solver status is deliberately not trusted.
		verification := verifier.VerifyRequest(req, report.Selection)
		plan := domain.Plan{
			ID:           newID(),
			Request:      req,
			Report:       report,
			Verification: verification,
			Consistent:   consistent(report, verification),
			CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.store.Save(plan); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, plan)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
	}
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == nil {
		http.Error(w, "frontend build is missing; run npm run build --prefix frontend", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(s.static, path); err != nil {
		r.URL.Path = "/"
		http.FileServer(http.FS(s.static)).ServeHTTP(w, r)
		return
	}
	http.FileServer(http.FS(s.static)).ServeHTTP(w, r)
}

// consistent compares the solver verdict with an independently computed
// verification report. Conflict conclusions must agree, and for feasible
// solutions the selected versions and error checks must match as well.
func consistent(report, verification domain.Report) bool {
	if report.Status != verification.Status {
		return false
	}
	if len(report.Selection) != len(verification.Selection) {
		return false
	}
	for name, version := range report.Selection {
		if verification.Selection[name] != version {
			return false
		}
	}
	for _, check := range verification.Checks {
		if check.Status == "error" {
			return report.Status == "conflict"
		}
	}
	return true
}

func decode(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func newID() string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

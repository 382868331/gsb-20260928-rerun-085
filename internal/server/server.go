package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
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
		saved.ExecutedState, saved.Irreversible = route.ExecutedSummary(req, report)
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
// a new route.
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
		if reflect.DeepEqual(normalizeChecks(step.Checks), normalizeChecks(payload.Checks)) {
			// Idempotent retry after an interrupted save: nothing is
			// executed again, the stored record is returned as-is.
			writeJSON(w, http.StatusOK, *saved)
			return
		}
		writeError(w, http.StatusConflict, "该步骤的核对记录已存在，不能被新记录覆盖")
		return
	}
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
	known := map[string]bool{}
	for _, check := range step.Checks {
		known[check.ID] = true
	}
	for _, check := range payload.Checks {
		if !known[check.ID] {
			writeError(w, http.StatusBadRequest, "unknown check id: "+check.ID)
			return
		}
	}
	step.Checks = append([]domain.StepCheck{}, payload.Checks...)
	allSuccess := true
	for _, check := range step.Checks {
		if check.Status != "success" {
			allSuccess = false
		}
	}
	step.Confirmed = true
	step.Effective = allSuccess
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
	if !allSuccess {
		// A step whose checks did not all succeed stays visible as held:
		// its record is preserved but it never counts as completed.
		saved.Report.HeldSteps = append(saved.Report.HeldSteps, *step)
	}
	saved.ExecutedState, saved.Irreversible = route.ExecutedSummary(saved.Request, saved.Report)
	if err := s.routes.Update(*saved); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, *saved)
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

func normalizeChecks(checks []domain.StepCheck) []domain.StepCheck {
	out := make([]domain.StepCheck, 0, len(checks))
	for _, check := range checks {
		out = append(out, domain.StepCheck{ID: check.ID, Status: check.Status, Note: check.Note})
	}
	return out
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

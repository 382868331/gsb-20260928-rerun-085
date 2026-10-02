package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/route"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/store"
	"dependency-workbench/internal/verifier"
)

type Server struct {
	store      *store.Store
	routeStore *store.RouteStore
	static     fs.FS
	mux        *http.ServeMux
}

func New(planStore *store.Store, routeStore *store.RouteStore, static fs.FS) *Server {
	s := &Server{store: planStore, routeStore: routeStore, static: static, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/scenarios", s.handleScenarios)
	s.mux.HandleFunc("/api/sample", s.handleSample)
	s.mux.HandleFunc("/api/solve", s.handleSolve)
	s.mux.HandleFunc("/api/plans/verify", s.handleVerify)
	s.mux.HandleFunc("/api/plans", s.handlePlans)
	s.mux.HandleFunc("/api/routes/plan", s.handleRoutePlan)
	s.mux.HandleFunc("/api/routes/advance", s.handleRouteAdvance)
	s.mux.HandleFunc("/api/routes", s.handleRoutes)
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

// handleRoutePlan plans a staged route for the request without persisting it.
func (s *Server) handleRoutePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req domain.SolveRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result := route.Plan(req, req.Current)
	writeJSON(w, http.StatusOK, planResponse(req, result))
}

type routePlanPayload struct {
	Steps           []domain.RouteStep `json:"steps"`
	Status          string             `json:"status"`
	FinalSelection  map[string]string  `json:"finalSelection,omitempty"`
	ConflictMessage string             `json:"conflictMessage,omitempty"`
	ConflictChain   []domain.ChainLink `json:"conflictChain,omitempty"`
}

func planResponse(req domain.SolveRequest, result route.Result) routePlanPayload {
	out := routePlanPayload{Steps: result.Steps, FinalSelection: result.FinalSelection}
	if result.Conflict != nil {
		out.Status = "conflict"
		out.ConflictMessage = result.Conflict.ConflictMessage
		out.ConflictChain = result.Conflict.ConflictChain
		if out.Steps == nil {
			out.Steps = []domain.RouteStep{}
		}
		return out
	}
	out.Status = "feasible"
	return out
}

// handleRoutes lists saved routes (GET) or plans and saves a new route (POST).
// Saved routes freeze the manifest snapshot; every step state is re-verified
// independently on read so a reopened workspace can still be explained.
func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		routes, err := s.routeStore.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range routes {
			reverifyRoute(&routes[i])
		}
		writeJSON(w, http.StatusOK, routes)
	case http.MethodPost:
		var req domain.SolveRequest
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result := route.Plan(req, req.Current)
		now := time.Now().UTC().Format(time.RFC3339)
		saved := domain.Route{
			ID:             newID(),
			Request:        req,
			Steps:          result.Steps,
			FinalSelection: result.FinalSelection,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if result.Conflict != nil {
			saved.Status = "blocked"
			saved.Steps = []domain.RouteStep{}
			saved.ConflictMessage = result.Conflict.ConflictMessage
			saved.ConflictChain = result.Conflict.ConflictChain
			saved.Rollback = route.AnalyzeRollback(req.Manifest, req.Current, nil, 0)
		} else {
			saved.Status = "feasible"
		}
		if err := s.routeStore.Save(saved); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
	}
}

type advanceRequest struct {
	ID             string                 `json:"id"`
	Confirm        int                    `json:"confirm"`
	Target         string                 `json:"target,omitempty"`
	DesiredVersion string                 `json:"desiredVersion,omitempty"`
	Goals          []domain.MigrationGoal `json:"goals,omitempty"`
	Locks          map[string]string      `json:"locks,omitempty"`
}

// handleRouteAdvance confirms a prefix of the remaining steps, optionally
// changes the final goal or locks not-yet-upgraded components, and replans
// only the steps after the confirmed state. Confirmed steps are frozen
// history and are never recomputed into a different route.
func (s *Server) handleRouteAdvance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var payload advanceRequest
	if err := decode(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, ok, err := s.routeStore.Get(payload.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown route")
		return
	}
	if payload.Confirm < 0 || saved.Confirmed+payload.Confirm > len(saved.Steps) {
		writeError(w, http.StatusBadRequest, "confirm exceeds the planned remaining steps")
		return
	}
	saved.Confirmed += payload.Confirm

	confirmedState := confirmedState(saved)
	req := saved.Request
	req.Current = confirmedState
	goalChanged := payload.Target != "" || len(payload.Goals) > 0 || payload.Locks != nil
	if payload.Target != "" || len(payload.Goals) > 0 {
		req.Target = payload.Target
		req.DesiredVersion = payload.DesiredVersion
		req.Goals = payload.Goals
	}
	if payload.Locks != nil {
		req.Locks = payload.Locks
	}
	// History is frozen by keeping confirmed steps verbatim and replanning
	// only from the confirmed state; no step before Confirmed is recomputed.
	result := route.Plan(req, confirmedState)
	history := append([]domain.RouteStep{}, saved.Steps[:saved.Confirmed]...)
	if result.Conflict != nil {
		saved.Steps = history
		saved.Status = "blocked"
		saved.ConflictMessage = result.Conflict.ConflictMessage
		saved.ConflictChain = result.Conflict.ConflictChain
		saved.FinalSelection = nil
	} else {
		for i := range result.Steps {
			result.Steps[i].Index = saved.Confirmed + i + 1
		}
		saved.Steps = append(history, result.Steps...)
		saved.Status = "feasible"
		saved.FinalSelection = result.FinalSelection
		saved.ConflictMessage = ""
		saved.ConflictChain = nil
	}
	saved.Rollback = route.AnalyzeRollback(req.Manifest, saved.Request.Current, history, saved.Confirmed)
	if goalChanged {
		saved.Revisions = append(saved.Revisions, domain.RouteRevision{
			ConfirmedSteps: saved.Confirmed,
			Goals:          req.Goals,
			Locks:          req.Locks,
			Note:           "goal or locks changed after confirming steps; remaining steps replanned from the confirmed state",
			At:             time.Now().UTC().Format(time.RFC3339),
		})
	}
	// Persist the effective goals/locks so a reopened route can explain the
	// remaining choices, while keeping the original current state as the
	// route starting point.
	saved.Request.Target = req.Target
	saved.Request.DesiredVersion = req.DesiredVersion
	saved.Request.Goals = req.Goals
	saved.Request.Locks = req.Locks
	saved.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if _, err := s.routeStore.Update(saved); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reverifyRoute(&saved)
	writeJSON(w, http.StatusOK, saved)
}

// confirmedState replays the confirmed steps over the original current state.
func confirmedState(saved domain.Route) map[string]string {
	state := map[string]string{}
	for name, version := range saved.Request.Current {
		state[name] = version
	}
	for _, step := range saved.Steps[:saved.Confirmed] {
		for _, change := range step.Changes {
			state[change.Component] = change.To
		}
	}
	return state
}

// reverifyRoute re-checks every step state against the frozen manifest
// snapshot so listed routes always carry a fresh independent verdict.
func reverifyRoute(saved *domain.Route) {
	for i := range saved.Steps {
		saved.Steps[i].Verification = verifier.CheckState(saved.Request.Manifest, saved.Steps[i].State)
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

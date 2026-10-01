package server

import (
	"crypto/sha256"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/store"
	"dependency-workbench/internal/verifier"
)

type Server struct {
	store  *store.Store
	static fs.FS
	mux    *http.ServeMux
}

func New(planStore *store.Store, static fs.FS) *Server {
	s := &Server{store: planStore, static: static, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/scenarios", s.handleScenarios)
	s.mux.HandleFunc("/api/sample", s.handleSample)
	s.mux.HandleFunc("/api/solve", s.handleSolve)
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

type verifyRequest struct {
	Manifest  domain.Manifest   `json:"manifest"`
	Selection map[string]string `json:"selection"`
	// Optional goal fields. When any of them is present the verifier also
	// checks that the selection actually covers the migration objectives, so
	// an empty or unrelated selection can no longer pass as feasible.
	Target         string            `json:"target,omitempty"`
	DesiredVersion string            `json:"desiredVersion,omitempty"`
	Locks          map[string]string `json:"locks,omitempty"`
	Adopted        map[string]string `json:"adopted,omitempty"`
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
	selection := payload.Selection
	if selection == nil {
		selection = map[string]string{}
	}
	goals := domain.SolveRequest{
		Target:         payload.Target,
		DesiredVersion: payload.DesiredVersion,
		Locks:          payload.Locks,
		Adopted:        payload.Adopted,
	}.Goals()
	writeJSON(w, http.StatusOK, verifier.VerifyWithGoals(payload.Manifest, selection, goals))
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		plans, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Never serve a borrowed conclusion: every plan is re-verified
		// against its own frozen snapshot each time it is read back, even
		// after a restart or a duplicate submission.
		for i := range plans {
			plans[i] = reverify(plans[i])
		}
		writeJSON(w, http.StatusOK, plans)
	case http.MethodPost:
		var req domain.SolveRequest
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		report := solver.Solve(req)
		plan := domain.Plan{
			ID:           newID(),
			Request:      req,
			Report:       report,
			ManifestHash: manifestHash(req.Manifest),
			CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		}
		plan = reverify(plan)
		if err := s.store.Save(plan); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, plan)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
	}
}

// reverify recomputes the independent verification from the plan's own
// request snapshot (manifest, goals and selection). Stored conclusions are
// treated as untrusted input so a plan can never inherit a success verdict
// produced for a different manifest revision or a different set of goals.
func reverify(plan domain.Plan) domain.Plan {
	plan.Verification = verifier.VerifyWithGoals(plan.Request.Manifest, plan.Report.Selection, plan.Request.Goals())
	if plan.ManifestHash == "" {
		plan.ManifestHash = manifestHash(plan.Request.Manifest)
	}
	plan.Consistent = consistent(plan.Report, plan.Verification)
	return plan
}

func manifestHash(manifest domain.Manifest) string {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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

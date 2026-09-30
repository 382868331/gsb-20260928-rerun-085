package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dependency-workbench/internal/domain"
	"dependency-workbench/internal/sample"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/store"
	"dependency-workbench/internal/verifier"
)

type server struct {
	store *store.Store
}

type verifyPayload struct {
	Manifest  domain.Manifest   `json:"manifest"`
	Selection map[string]string `json:"selection"`
	Target    string            `json:"target,omitempty"`
	Desired   string            `json:"desiredVersion,omitempty"`
	Locks     map[string]string `json:"locks,omitempty"`
}

func verificationRoots(req domain.SolveRequest) []domain.Requirement {
	roots := []domain.Requirement{{Component: req.Target, Constraint: "=" + req.DesiredVersion}}
	for name, version := range req.Locks {
		roots = append(roots, domain.Requirement{Component: name, Constraint: "=" + version})
	}
	return roots
}

func verificationRootsFromPayload(payload verifyPayload) []domain.Requirement {
	roots := []domain.Requirement{}
	if payload.Target != "" {
		constraint := payload.Desired
		if constraint == "" {
			constraint = "*"
		}
		roots = append(roots, domain.Requirement{Component: payload.Target, Constraint: "=" + constraint})
	}
	for name, version := range payload.Locks {
		roots = append(roots, domain.Requirement{Component: name, Constraint: "=" + version})
	}
	return roots
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataPath := flag.String("data", envOr("PLAN_STORE", "/data/plans.json"), "path to persisted plans")
	staticDir := flag.String("static", envOr("STATIC_DIR", "frontend/dist"), "directory with built frontend assets")
	flag.Parse()

	planStore, err := store.New(*dataPath)
	if err != nil {
		log.Fatalf("open plan store: %v", err)
	}

	s := &server{store: planStore}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sample", s.handleSample)
	mux.HandleFunc("/api/solve", s.handleSolve)
	mux.HandleFunc("/api/plans/verify", s.handleVerify)
	mux.HandleFunc("/api/plans", s.handlePlans)

	fs := http.FileServer(http.Dir(*staticDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if full := filepath.Join(*staticDir, filepath.Clean(r.URL.Path)); fileExists(full) {
				fs.ServeHTTP(w, r)
				return
			}
		}
		index := filepath.Join(*staticDir, "index.html")
		if fileExists(index) {
			http.ServeFile(w, r, index)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("API is running. Build the frontend with: npm run build --prefix frontend\n"))
	})

	log.Printf("dependency workbench listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func (s *server) handleSample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	writeJSON(w, http.StatusOK, sample.Get(r.URL.Query().Get("scenario")))
}

func (s *server) handleSolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req domain.SolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, solver.Solve(req))
}

func (s *server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var payload verifyPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, verifier.Verify(payload.Manifest, payload.Selection, verificationRootsFromPayload(payload)...))
}

func (s *server) handlePlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		plans, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read plans: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, plans)
	case http.MethodPost:
		var req domain.SolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		// Re-run the solver and, separately, the verifier. The plan is stored with
		// both reports plus the exact request snapshot, so saved plans stay
		// independently checkable and immutable under later constraint edits.
		report := solver.Solve(req)
		verified := verifier.Verify(req.Manifest, report.Selection, verificationRoots(req)...)
		consistent := report.Status == verified.Status
		plan := domain.Plan{
			ID:         newID(),
			Request:    req,
			Report:     report,
			Verify:     verified,
			Consistent: consistent,
			CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.store.Save(plan); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save plan: "+err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, plan)
	default:
		writeError(w, http.StatusMethodNotAllowed, "use GET or POST")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func newID() string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

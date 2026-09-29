package main

import (
	"dependency-workbench/internal/model"
	"dependency-workbench/internal/solver"
	"dependency-workbench/internal/store"
	"dependency-workbench/internal/verifier"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type Server struct {
	catalog model.Catalog
	engine  *solver.Engine
	index   verifier.CatalogIndex
	plans   *store.Store
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	mux := http.NewServeMux()
	server, err := newServer()
	if err != nil {
		return err
	}
	mux.HandleFunc("GET /api/catalog", server.catalogHandler)
	mux.HandleFunc("POST /api/solve", server.solveHandler)
	mux.HandleFunc("POST /api/verify", server.verifyHandler)
	mux.HandleFunc("GET /api/plans", server.listPlansHandler)
	mux.HandleFunc("POST /api/plans", server.savePlanHandler)
	mux.HandleFunc("GET /api/plans/", server.getPlanHandler)
	fileServer := http.FileServer(http.Dir("frontend/dist"))
	mux.Handle("/", fileServer)
	log.Print("listening on http://localhost:8080")
	return http.ListenAndServe(":8080", withCORS(mux))
}

func newServer() (*Server, error) {
	raw, err := os.ReadFile("data/catalog.json")
	if err != nil {
		return nil, err
	}
	var catalog model.Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	engine, err := solver.New(catalog)
	if err != nil {
		return nil, err
	}
	return &Server{
		catalog: catalog,
		engine:  engine,
		index:   verifier.Index(catalog),
		plans:   store.New("data/plans.json"),
	}, nil
}

func (server *Server) catalogHandler(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, server.catalog)
}

func (server *Server) solveHandler(writer http.ResponseWriter, request *http.Request) {
	var input model.SolveRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	choices, reasons, optional, conflicts := server.engine.Solve(input)
	if choices == nil {
		status := http.StatusConflict
		if len(conflicts) == 0 {
			conflicts = []model.Conflict{{Kind: "unsatisfiable", Message: "找不到满足所有约束的版本组合"}}
		}
		writeJSON(writer, status, model.Report{Satisfied: false, Choices: map[string]string{}, Conflicts: conflicts, Messages: []string{"约束不可满足，请调整目标版本或锁定项"}})
		return
	}
	report := server.index.Verify(choices, input.Current, input.TargetComponent)
	applyReasons(&report, reasons, optional, input.TargetComponent)
	writeJSON(writer, http.StatusOK, report)
}

func applyReasons(report *model.Report, reasons map[string]string, optional map[string]bool, target string) {
	for index := range report.Changes {
		change := &report.Changes[index]
		change.Reason = reasons[change.Component]
		change.IsOptional = optional[change.Component]
		change.IsTarget = change.Component == target
	}
}

type verifyRequest struct {
	Choices map[string]string `json:"choices"`
	Current map[string]string `json:"current"`
	Target  string            `json:"target"`
}

func (server *Server) verifyHandler(writer http.ResponseWriter, request *http.Request) {
	var input verifyRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	report := server.index.Verify(input.Choices, input.Current, input.Target)
	writeJSON(writer, http.StatusOK, report)
}

func (server *Server) listPlansHandler(writer http.ResponseWriter, request *http.Request) {
	plans, err := server.plans.List()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, plans)
}

func (server *Server) savePlanHandler(writer http.ResponseWriter, request *http.Request) {
	var plan model.Plan
	if err := json.NewDecoder(request.Body).Decode(&plan); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if len(plan.Choices) == 0 {
		writeError(writer, http.StatusBadRequest, "方案不能为空")
		return
	}
	saved, err := server.plans.Save(plan)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	report := server.index.Verify(saved.Choices, saved.Current, saved.Target)
	writeJSON(writer, http.StatusOK, map[string]any{"plan": saved, "report": report})
}

func (server *Server) getPlanHandler(writer http.ResponseWriter, request *http.Request) {
	id := strings.TrimPrefix(request.URL.Path, "/api/plans/")
	plan, err := server.plans.Get(id)
	if err != nil {
		writeError(writer, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, plan)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

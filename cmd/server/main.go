package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

	"dependency-workbench/internal/server"
	"dependency-workbench/internal/store"
)

//go:embed all:static
var staticFS embed.FS

func main() {
	planPath := envOr("PLANS_PATH", "plans.json")
	routePath := envOr("ROUTES_PATH", "routes.json")
	addr := envOr("ADDR", ":8080")

	planStore, err := store.New(planPath)
	if err != nil {
		log.Fatalf("open plan store: %v", err)
	}
	routeStore, err := store.NewRouteStore(routePath)
	if err != nil {
		log.Fatalf("open route store: %v", err)
	}
	webFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("open embedded frontend: %v", err)
	}
	handler := server.New(planStore, routeStore, webFS)
	log.Printf("dependency workbench listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

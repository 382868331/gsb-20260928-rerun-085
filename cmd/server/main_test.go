package main

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"dependency-workbench/internal/server"
	"dependency-workbench/internal/store"
)

func TestEmbeddedFrontendAndAPI(t *testing.T) {
	planStore, err := store.New(filepath.Join(t.TempDir(), "plans.json"))
	if err != nil {
		t.Fatal(err)
	}
	routeStore, err := store.NewRouteStore(filepath.Join(t.TempDir(), "routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	webFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		t.Fatal(err)
	}
	handler := server.New(planStore, routeStore, webFS)
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "server.sock"))
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer listener.Close()
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", listener.Addr().String())
		},
	}}
	get := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, "http://workbench"+path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		return resp.StatusCode, string(body[:n])
	}
	if status, body := get("/"); status != http.StatusOK || !contains(body, "<div id=\"root\">") {
		t.Fatalf("index page status=%d body=%q", status, body)
	}
	if status, _ := get("/api/scenarios"); status != http.StatusOK {
		t.Fatalf("scenarios status=%d", status)
	}
	if status, body := get("/nonexistent/spa/route"); status != http.StatusOK || !contains(body, "<div id=\"root\">") {
		t.Fatalf("SPA fallback status=%d body=%q", status, body)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

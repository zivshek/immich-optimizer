package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardHandlers(t *testing.T) {
	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Record("alice", "photo.jxl", "4032x3024", 1000, 500); err != nil {
		t.Fatal(err)
	}

	logs := NewLogBuffer(10)
	_, _ = logs.Write([]byte("processing photo.jpg\n"))
	dashboard := NewDashboardServer(":0", store, logs, &strings.Builder{})

	statsRequest := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	statsResponse := httptest.NewRecorder()
	dashboard.handleStats(statsResponse, statsRequest)
	if statsResponse.Code != http.StatusOK || !strings.Contains(statsResponse.Body.String(), `"saved_bytes":500`) {
		t.Fatalf("unexpected stats response: %d %s", statsResponse.Code, statsResponse.Body.String())
	}

	logRequest := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	logResponse := httptest.NewRecorder()
	dashboard.handleLogs(logResponse, logRequest)
	if !strings.Contains(logResponse.Body.String(), "processing photo.jpg") {
		t.Fatalf("unexpected logs response: %s", logResponse.Body.String())
	}

	indexRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	indexResponse := httptest.NewRecorder()
	dashboard.handleIndex(indexResponse, indexRequest)
	for _, expected := range []string{`id="copy-log"`, "navigator.clipboard.writeText", `class="dashboard-section"`} {
		if !strings.Contains(indexResponse.Body.String(), expected) {
			t.Fatalf("dashboard HTML is missing %q", expected)
		}
	}
}

package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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
	if err := store.RecordFailure("alice", "failed.mp4", "2160x3840", 2000, fmt.Errorf("failed job")); err != nil {
		t.Fatal(err)
	}

	logs := NewLogBuffer(10)
	_, _ = logs.Write([]byte("processing photo.jpg\n"))
	configRoot := t.TempDir()
	configDir := filepath.Join(configRoot, "standard", "lossless")
	if err := os.MkdirAll(configDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tasks.yaml"), []byte(`
tasks:
  - name: images
    command: ""
    extensions: [jpg]
  - name: videos
    command: ""
    extensions: [mp4]
`), 0600); err != nil {
		t.Fatal(err)
	}
	registry := NewTaskConfigRegistry(configRoot, filepath.Join(t.TempDir(), "missing-custom"), store)
	configFile := filepath.Join(configDir, "tasks.yaml")
	config, err := NewConfig(&configFile)
	if err != nil {
		t.Fatal(err)
	}
	profile := &ProfileConfig{Name: "alice", ConfigFile: configFile, Tasks: config}
	watcher := &FileWatcher{
		profile: profile, config: profile.Tasks, logger: log.New(io.Discard, "", 0),
		imageConfig: taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
		videoConfig: taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
	}
	if err := registry.Register(watcher); err != nil {
		t.Fatal(err)
	}
	dashboard := NewDashboardServer(":0", store, registry, logs, &strings.Builder{})

	statsRequest := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	statsResponse := httptest.NewRecorder()
	dashboard.handleStats(statsResponse, statsRequest)
	if statsResponse.Code != http.StatusOK || !strings.Contains(statsResponse.Body.String(), `"saved_bytes":500`) {
		t.Fatalf("unexpected stats response: %d %s", statsResponse.Code, statsResponse.Body.String())
	}

	configRequest := httptest.NewRequest(http.MethodGet, "/api/task-configs", nil)
	configResponse := httptest.NewRecorder()
	dashboard.handleTaskConfigs(configResponse, configRequest)
	if configResponse.Code != http.StatusOK || !strings.Contains(configResponse.Body.String(), `"image_current":"standard/lossless"`) {
		t.Fatalf("unexpected task configs response: %d %s", configResponse.Code, configResponse.Body.String())
	}
	selectRequest := httptest.NewRequest(http.MethodPut, "/api/task-configs/alice", strings.NewReader(`{"image_config":"standard/lossless","image_score":87,"video_config":"standard/lossless","video_score":93,"use_nvidia":true}`))
	selectRequest.SetPathValue("profile", "alice")
	selectResponse := httptest.NewRecorder()
	dashboard.handleSelectTaskConfig(selectResponse, selectRequest)
	if selectResponse.Code != http.StatusNoContent {
		t.Fatalf("unexpected task config selection response: %d %s", selectResponse.Code, selectResponse.Body.String())
	}
	if !watcher.nvidiaEnabled() {
		t.Fatal("NVIDIA option was not applied")
	}
	if watcher.currentImageScore() != 87 {
		t.Fatalf("image score was not applied: %d", watcher.currentImageScore())
	}
	if watcher.currentVideoScore() != 93 {
		t.Fatalf("video score was not applied: %d", watcher.currentVideoScore())
	}

	recentRequest := httptest.NewRequest(http.MethodGet, "/api/recent?page=1", nil)
	recentResponse := httptest.NewRecorder()
	dashboard.handleRecent(recentResponse, recentRequest)
	if recentResponse.Code != http.StatusOK || !strings.Contains(recentResponse.Body.String(), `"total":2`) || !strings.Contains(recentResponse.Body.String(), `"success":false`) {
		t.Fatalf("unexpected recent response: %d %s", recentResponse.Code, recentResponse.Body.String())
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/recent/1", nil)
	deleteRequest.SetPathValue("id", "1")
	deleteResponse := httptest.NewRecorder()
	dashboard.handleDeleteRecent(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("unexpected delete response: %d %s", deleteResponse.Code, deleteResponse.Body.String())
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
	for _, expected := range []string{`id="copy-log"`, "navigator.clipboard.writeText", `class="dashboard-section"`, "Media Configurations", "Image Config", "Image Score", "Video Config", "Video Score", "Use when supported", "taskConfigs.addEventListener('change'", "config-control"} {
		if !strings.Contains(indexResponse.Body.String(), expected) {
			t.Fatalf("dashboard HTML is missing %q", expected)
		}
	}
	if strings.Contains(indexResponse.Body.String(), "apply-config") {
		t.Fatal("dashboard HTML still contains the Apply button")
	}
}

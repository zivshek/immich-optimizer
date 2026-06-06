package main

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessFileDeletesOriginalOnlyAfterSuccessfulUpload(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		wantExists bool
	}{
		{name: "success", statusCode: http.StatusCreated, wantExists: false},
		{name: "failure", statusCode: http.StatusInternalServerError, wantExists: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("x-api-key"); got != "profile-api-key" {
					t.Errorf("unexpected API key: %q", got)
				}
				w.WriteHeader(test.statusCode)
			}))
			defer server.Close()

			watchDir := t.TempDir()
			undoneDir := t.TempDir()
			filePath := filepath.Join(watchDir, "photo.jpg")
			if err := os.WriteFile(filePath, []byte("photo"), 0600); err != nil {
				t.Fatal(err)
			}

			logger := log.New(os.Stderr, "", 0)
			profile := &ProfileConfig{
				Name:       "alice",
				WatchDir:   watchDir,
				UndoneDir:  undoneDir,
				ConfigFile: filepath.Join(t.TempDir(), "tasks.yaml"),
				Tasks:      &Config{},
			}
			watcher := &FileWatcher{
				watchDir:     watchDir,
				immichClient: NewImmichClient(server.URL, "profile-api-key", 5, newCustomLogger(logger, "")),
				config:       profile.Tasks,
				logger:       logger,
				profile:      profile,
			}

			watcher.processFile(filePath)

			_, err := os.Stat(filePath)
			if test.wantExists && err != nil {
				t.Fatalf("original should remain after failed upload: %v", err)
			}
			if !test.wantExists && !os.IsNotExist(err) {
				t.Fatalf("original should be deleted after successful upload, stat error: %v", err)
			}
		})
	}
}

func TestTaskProcessorAcceptsPassthroughTask(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(filePath, []byte("photo"), 0600); err != nil {
		t.Fatal(err)
	}
	processor, err := NewTaskProcessor(filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer processor.Close()

	task := Task{Name: "passthrough", Extensions: []string{"jpg"}, Command: ""}
	if err := task.Init(); err != nil {
		t.Fatal(err)
	}
	if err := processor.Process([]Task{task}); err != nil {
		t.Fatalf("passthrough task failed: %v", err)
	}
	if processor.ProcessedFile != nil {
		t.Fatal("passthrough task unexpectedly created a processed file")
	}
}

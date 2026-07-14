package main

import (
	"bytes"
	"io"
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
			statsStore, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer statsStore.Close()
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
				statsStore:   statsStore,
			}

			watcher.processFile(filePath)

			_, err = os.Stat(filePath)
			if test.wantExists && err != nil {
				t.Fatalf("original should remain after failed upload: %v", err)
			}
			if !test.wantExists && !os.IsNotExist(err) {
				t.Fatalf("original should be deleted after successful upload, stat error: %v", err)
			}

			stats, err := statsStore.Summary()
			if err != nil {
				t.Fatal(err)
			}
			recent, err := statsStore.Recent(10)
			if err != nil {
				t.Fatal(err)
			}
			if test.statusCode == http.StatusCreated {
				if stats.ProcessedCount != 1 || len(recent) != 1 || !recent[0].Success {
					t.Fatalf("unexpected successful job history: stats=%+v recent=%+v", stats, recent)
				}
			} else if stats.ProcessedCount != 0 || len(recent) != 1 || recent[0].Success {
				t.Fatalf("unexpected failed job history: stats=%+v recent=%+v", stats, recent)
			}
		})
	}
}

func TestProcessFileUploadsOriginalAfterOptimizationFailure(t *testing.T) {
	for _, test := range []struct {
		name        string
		statusCode  int
		wantExists  bool
		wantSuccess bool
	}{
		{name: "fallback upload succeeds", statusCode: http.StatusCreated, wantExists: false, wantSuccess: true},
		{name: "fallback upload fails", statusCode: http.StatusInternalServerError, wantExists: true, wantSuccess: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var uploaded []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				uploaded = body
				w.WriteHeader(test.statusCode)
			}))
			defer server.Close()

			watchDir := t.TempDir()
			undoneDir := t.TempDir()
			filePath := filepath.Join(watchDir, "photo.jpg")
			original := []byte("original-photo")
			if err := os.WriteFile(filePath, original, 0600); err != nil {
				t.Fatal(err)
			}
			task := Task{Name: "always-fails", Extensions: []string{"jpg"}, Command: "exit 1"}
			if err := task.Init(); err != nil {
				t.Fatal(err)
			}
			config := &Config{Tasks: []Task{task}}
			statsStore, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer statsStore.Close()
			logger := log.New(io.Discard, "", 0)
			profile := &ProfileConfig{
				Name:      "alice",
				WatchDir:  watchDir,
				UndoneDir: undoneDir,
				Tasks:     config,
			}
			watcher := &FileWatcher{
				watchDir:     watchDir,
				immichClient: NewImmichClient(server.URL, "profile-api-key", 5, newCustomLogger(logger, "")),
				config:       config,
				logger:       logger,
				profile:      profile,
				statsStore:   statsStore,
			}

			watcher.processFile(filePath)

			_, statErr := os.Stat(filePath)
			if test.wantExists && statErr != nil {
				t.Fatalf("original should remain after failed fallback upload: %v", statErr)
			}
			if !test.wantExists && !os.IsNotExist(statErr) {
				t.Fatalf("original should be deleted after successful fallback upload: %v", statErr)
			}
			if test.wantSuccess && !bytes.Contains(uploaded, original) {
				t.Fatal("fallback upload did not contain original file bytes")
			}
			recent, err := statsStore.Recent(10)
			if err != nil {
				t.Fatal(err)
			}
			if len(recent) != 1 || recent[0].Success != test.wantSuccess {
				t.Fatalf("unexpected fallback job history: %+v", recent)
			}
			if test.wantSuccess && (recent[0].OriginalBytes != int64(len(original)) || recent[0].UploadedBytes != int64(len(original))) {
				t.Fatalf("successful fallback should record zero reduction: %+v", recent[0])
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

func TestShouldUploadProcessedFileRequiresImageSavingsThreshold(t *testing.T) {
	watcher := &FileWatcher{logger: log.New(io.Discard, "", 0)}
	tests := []struct {
		name              string
		originalExtension string
		originalSize      int64
		processedSize     int64
		want              bool
	}{
		{
			name:              "image below threshold",
			originalExtension: ".jpg",
			originalSize:      1000,
			processedSize:     860,
			want:              false,
		},
		{
			name:              "image at threshold",
			originalExtension: ".jpg",
			originalSize:      1000,
			processedSize:     850,
			want:              true,
		},
		{
			name:              "image above threshold",
			originalExtension: ".heic",
			originalSize:      1000,
			processedSize:     800,
			want:              true,
		},
		{
			name:              "video keeps any savings",
			originalExtension: ".mp4",
			originalSize:      1000,
			processedSize:     990,
			want:              true,
		},
		{
			name:              "larger output is rejected",
			originalExtension: ".jpg",
			originalSize:      1000,
			processedSize:     1000,
			want:              false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &TaskProcessor{
				OriginalExtension: test.originalExtension,
				OriginalSize:      test.originalSize,
				ProcessedFile:     os.Stdin,
				ProcessedSize:     test.processedSize,
			}
			if got := watcher.shouldUploadProcessedFile(processor); got != test.want {
				t.Fatalf("shouldUploadProcessedFile() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIsHiddenPath(t *testing.T) {
	watchDir := t.TempDir()
	watcher := &FileWatcher{watchDir: watchDir}

	tests := []struct {
		path       string
		wantHidden bool
	}{
		{path: filepath.Join(watchDir, ".trashed-video.mp4"), wantHidden: true},
		{path: filepath.Join(watchDir, "Camera", ".trashed-video.mp4"), wantHidden: true},
		{path: filepath.Join(watchDir, ".thumbnails", "photo.jpg"), wantHidden: true},
		{path: filepath.Join(watchDir, "Camera", "photo.jpg"), wantHidden: false},
	}

	for _, test := range tests {
		if got := watcher.isHiddenPath(test.path); got != test.wantHidden {
			t.Errorf("isHiddenPath(%q) = %v, want %v", test.path, got, test.wantHidden)
		}
	}
}

func TestProcessFileIgnoresHiddenFile(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	watchDir := t.TempDir()
	filePath := filepath.Join(watchDir, ".trashed-photo.jpg")
	if err := os.WriteFile(filePath, []byte("photo"), 0600); err != nil {
		t.Fatal(err)
	}

	logger := log.New(os.Stderr, "", 0)
	watcher := &FileWatcher{
		watchDir:     watchDir,
		immichClient: NewImmichClient(server.URL, "profile-api-key", 5, newCustomLogger(logger, "")),
		config:       &Config{},
		logger:       logger,
	}

	watcher.processFile(filePath)

	if requests != 0 {
		t.Fatalf("hidden file triggered %d upload requests", requests)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("hidden file should remain untouched: %v", err)
	}
}

func TestParseMediaResolutionUsesDisplayedDimensions(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "unrotated",
			json: `{"streams":[{"width":3840,"height":2160}]}`,
			want: "3840x2160",
		},
		{
			name: "negative ninety",
			json: `{"streams":[{"width":3840,"height":2160,"side_data_list":[{"rotation":-90}]}]}`,
			want: "2160x3840",
		},
		{
			name: "positive ninety",
			json: `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":90}]}]}`,
			want: "1080x1920",
		},
		{
			name: "one eighty",
			json: `{"streams":[{"width":3840,"height":2160,"side_data_list":[{"rotation":180}]}]}`,
			want: "3840x2160",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseMediaResolution([]byte(test.json)); got != test.want {
				t.Fatalf("parseMediaResolution() = %q, want %q", got, test.want)
			}
		})
	}
}

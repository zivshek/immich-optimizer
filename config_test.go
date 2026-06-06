package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewProfilesConfigExpandsEnvironmentAndResolvesPaths(t *testing.T) {
	t.Setenv("TEST_IMMICH_KEY", "test-api-key-long-enough")
	dir := t.TempDir()
	profilesFile := filepath.Join(dir, "profiles.yaml")
	content := `
profiles:
  - name: alice
    immich_url: http://immich:2283
    immich_api_key: ${TEST_IMMICH_KEY}
    watch_dir: inbox/alice
    undone_dir: undone/alice
    tasks_file: tasks.yaml
`
	if err := os.WriteFile(profilesFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	profiles, err := NewProfilesConfig(profilesFile)
	if err != nil {
		t.Fatal(err)
	}

	profile := profiles.Profiles[0]
	if profile.ImmichAPIKey != "test-api-key-long-enough" {
		t.Fatalf("API key was not expanded: %q", profile.ImmichAPIKey)
	}
	if profile.WatchDir != filepath.Join(dir, "inbox", "alice") {
		t.Fatalf("watch directory was not resolved: %q", profile.WatchDir)
	}
}

func TestNewProfilesConfigRejectsOverlappingWatchDirectories(t *testing.T) {
	dir := t.TempDir()
	profilesFile := filepath.Join(dir, "profiles.yaml")
	content := `
profiles:
  - name: alice
    immich_url: http://immich:2283
    immich_api_key: test-api-key-for-alice
    watch_dir: inbox
    undone_dir: undone/alice
    tasks_file: tasks.yaml
  - name: bob
    immich_url: http://immich:2283
    immich_api_key: test-api-key-for-bob
    watch_dir: inbox/bob
    undone_dir: undone/bob
    tasks_file: tasks.yaml
`
	if err := os.WriteFile(profilesFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := NewProfilesConfig(profilesFile)
	if err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

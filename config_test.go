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

func TestNewProfilesFromEnvironmentUsesSharedSettings(t *testing.T) {
	t.Setenv("IUO_PROFILE_HSHI_API_KEY", "test-api-key-long-enough")
	t.Setenv("IUO_PROFILE_HSHI_WATCH_DIR", "/inbox/hshi")
	t.Setenv("IUO_PROFILE_HSHI_UNDONE_DIR", "/undone/hshi")

	profiles, err := NewProfilesFromEnvironment(
		"hshi",
		"http://immich:2283",
		"/etc/immich-optimizer/bundled-configs/storage-saver-nvidia-gpu/tasks.yaml",
	)
	if err != nil {
		t.Fatal(err)
	}

	profile := profiles.Profiles[0]
	if profile.ImmichURL != "http://immich:2283" {
		t.Fatalf("unexpected shared Immich URL: %q", profile.ImmichURL)
	}
	if profile.ConfigFile != "/etc/immich-optimizer/bundled-configs/storage-saver-nvidia-gpu/tasks.yaml" {
		t.Fatalf("unexpected shared tasks file: %q", profile.ConfigFile)
	}
	if profile.ImmichAPIKey != "test-api-key-long-enough" {
		t.Fatalf("unexpected profile API key: %q", profile.ImmichAPIKey)
	}
}

func TestNormalizeProfileEnvName(t *testing.T) {
	if got := normalizeProfileEnvName("Jane-Doe"); got != "JANE_DOE" {
		t.Fatalf("unexpected normalized name: %q", got)
	}
}

func TestNewProfilesFromInlineConfigUsesSharedSettings(t *testing.T) {
	config := `
profiles:
  - user: hshi
    api_key: test-api-key-long-enough
    watch_dir: /inbox/hshi
    undone_dir: /undone/hshi
`

	profiles, err := NewProfilesFromInlineConfig(
		config,
		"http://immich:2283",
		"/etc/immich-optimizer/bundled-configs/storage-saver-nvidia-gpu/tasks.yaml",
	)
	if err != nil {
		t.Fatal(err)
	}

	profile := profiles.Profiles[0]
	if profile.Name != "hshi" {
		t.Fatalf("unexpected profile name: %q", profile.Name)
	}
	if profile.ImmichURL != "http://immich:2283" {
		t.Fatalf("unexpected shared Immich URL: %q", profile.ImmichURL)
	}
	if profile.ConfigFile != "/etc/immich-optimizer/bundled-configs/storage-saver-nvidia-gpu/tasks.yaml" {
		t.Fatalf("unexpected shared tasks file: %q", profile.ConfigFile)
	}
	if profile.ImmichAPIKey != "test-api-key-long-enough" {
		t.Fatalf("unexpected profile API key: %q", profile.ImmichAPIKey)
	}
}

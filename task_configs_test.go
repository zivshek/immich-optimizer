package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskConfigRegistrySelectsImageAndVideoConfigsIndependently(t *testing.T) {
	root := t.TempDir()
	writeTestTaskConfig(t, root, "standard/alpha", []byte(`
tasks:
  - name: images
    command: ""
    extensions: [jpg]
  - name: videos
    command: ""
    extensions: [mp4]
`))
	writeTestTaskConfig(t, root, "standard/zeta", []byte(`
tasks:
  - name: videos
    command: ""
    extensions: [mp4]
`))
	customRoot := t.TempDir()
	writeTestTaskConfig(t, customRoot, "my-profile", []byte(`
tasks:
  - name: images
    command: ""
    extensions: [jpg]
`))

	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	configFile := filepath.Join(root, "standard", "alpha", "tasks.yaml")
	startupConfig, err := NewConfig(&configFile)
	if err != nil {
		t.Fatal(err)
	}
	profile := &ProfileConfig{Name: "alice", ConfigFile: configFile, Tasks: startupConfig}
	watcher := &FileWatcher{
		profile: profile, config: profile.Tasks, logger: log.New(io.Discard, "", 0),
		imageConfig: taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
		videoConfig: taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
	}
	registry := NewTaskConfigRegistry(root, customRoot, store)
	if err := registry.Register(watcher); err != nil {
		t.Fatal(err)
	}

	profiles, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].ImageCurrent != "standard/alpha" || profiles[0].VideoCurrent != "standard/alpha" {
		t.Fatalf("unexpected initial configs: %+v", profiles)
	}
	if len(profiles[0].ImageConfigs) != 2 || profiles[0].ImageConfigs[0].Name != "custom/my-profile" {
		t.Fatalf("unexpected image configs: %+v", profiles[0].ImageConfigs)
	}
	if len(profiles[0].VideoConfigs) != 2 || profiles[0].VideoConfigs[0].Name != "standard/alpha" {
		t.Fatalf("unexpected video configs: %+v", profiles[0].VideoConfigs)
	}

	if err := registry.Select("alice", mediaTypeImage, "custom/my-profile"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Select("alice", mediaTypeVideo, "standard/zeta"); err != nil {
		t.Fatal(err)
	}
	profiles, err = registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].ImageCurrent != "custom/my-profile" || profiles[0].VideoCurrent != "standard/zeta" {
		t.Fatalf("independent selections were not applied: %+v", profiles)
	}
	imageSelected, err := store.SelectedMediaTaskConfig("alice", mediaTypeImage)
	if err != nil || imageSelected != "custom/my-profile" {
		t.Fatalf("image selection was not persisted: %q, %v", imageSelected, err)
	}
	videoSelected, err := store.SelectedMediaTaskConfig("alice", mediaTypeVideo)
	if err != nil || videoSelected != "standard/zeta" {
		t.Fatalf("video selection was not persisted: %q, %v", videoSelected, err)
	}
}

func writeTestTaskConfig(t *testing.T, root, name string, contents []byte) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), contents, 0600); err != nil {
		t.Fatal(err)
	}
}

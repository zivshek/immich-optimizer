package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskConfigRegistrySelectsAndOrdersRecentConfigs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"standard/zeta", "standard/alpha"} {
		dir := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tasks.yaml"), []byte("tasks: []\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStatsStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	profile := &ProfileConfig{Name: "alice", ConfigFile: filepath.Join(root, "standard", "alpha", "tasks.yaml"), Tasks: &Config{}}
	watcher := &FileWatcher{profile: profile, config: profile.Tasks, configFile: profile.ConfigFile, configName: profile.ConfigFile, logger: log.New(io.Discard, "", 0)}
	registry := NewTaskConfigRegistry(root, store)
	if err := registry.Register(watcher); err != nil {
		t.Fatal(err)
	}

	profiles, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].Current != "standard/alpha" || profiles[0].Configs[0].Name != "standard/alpha" {
		t.Fatalf("unexpected initial configs: %+v", profiles)
	}

	if err := registry.Select("alice", "standard/zeta"); err != nil {
		t.Fatal(err)
	}
	profiles, err = registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].Current != "standard/zeta" || profiles[0].Configs[0].Name != "standard/zeta" {
		t.Fatalf("recent config was not promoted: %+v", profiles)
	}
	selected, err := store.SelectedTaskConfig("alice")
	if err != nil || selected != "standard/zeta" {
		t.Fatalf("selection was not persisted: %q, %v", selected, err)
	}
}

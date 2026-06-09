package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type BundledTaskConfig struct {
	Name     string     `json:"name"`
	LastUsed *time.Time `json:"last_used,omitempty"`
}

type ProfileTaskConfigs struct {
	Profile string              `json:"profile"`
	Current string              `json:"current"`
	Configs []BundledTaskConfig `json:"configs"`
}

type TaskConfigRegistry struct {
	root     string
	store    *StatsStore
	mu       sync.RWMutex
	watchers map[string]*FileWatcher
}

func NewTaskConfigRegistry(root string, store *StatsStore) *TaskConfigRegistry {
	return &TaskConfigRegistry{
		root:     filepath.Clean(root),
		store:    store,
		watchers: make(map[string]*FileWatcher),
	}
}

func (registry *TaskConfigRegistry) Register(watcher *FileWatcher) error {
	registry.mu.Lock()
	registry.watchers[watcher.profile.Name] = watcher
	registry.mu.Unlock()

	selected, err := registry.store.SelectedTaskConfig(watcher.profile.Name)
	if err != nil {
		return err
	}
	if selected == "" {
		configPaths, discoverErr := registry.discover()
		if discoverErr != nil {
			return discoverErr
		}
		for name, path := range configPaths {
			if filepath.Clean(path) == filepath.Clean(watcher.profile.ConfigFile) {
				watcher.configMu.Lock()
				watcher.configName = name
				watcher.configFile = path
				watcher.configMu.Unlock()
				break
			}
		}
		return nil
	}
	configPaths, err := registry.discover()
	if err != nil {
		return err
	}
	configPath, ok := configPaths[selected]
	if !ok {
		return fmt.Errorf("saved bundled task config %q not found", selected)
	}
	config, err := NewConfig(&configPath)
	if err != nil {
		return fmt.Errorf("load saved bundled task config %q: %w", selected, err)
	}
	watcher.setConfig(config, configPath, selected)
	return nil
}

func (registry *TaskConfigRegistry) List() ([]ProfileTaskConfigs, error) {
	configPaths, err := registry.discover()
	if err != nil {
		return nil, err
	}
	usage, err := registry.store.TaskConfigUsage()
	if err != nil {
		return nil, err
	}

	configs := make([]BundledTaskConfig, 0, len(configPaths))
	for name := range configPaths {
		config := BundledTaskConfig{Name: name}
		if used, ok := usage[name]; ok {
			usedCopy := used
			config.LastUsed = &usedCopy
		}
		configs = append(configs, config)
	}
	sort.Slice(configs, func(i, j int) bool {
		a, b := configs[i], configs[j]
		switch {
		case a.LastUsed != nil && b.LastUsed != nil && !a.LastUsed.Equal(*b.LastUsed):
			return a.LastUsed.After(*b.LastUsed)
		case a.LastUsed != nil && b.LastUsed == nil:
			return true
		case a.LastUsed == nil && b.LastUsed != nil:
			return false
		default:
			return a.Name < b.Name
		}
	})

	registry.mu.RLock()
	defer registry.mu.RUnlock()
	profiles := make([]ProfileTaskConfigs, 0, len(registry.watchers))
	for name, watcher := range registry.watchers {
		current := watcher.currentConfigName()
		profileConfigs := make([]BundledTaskConfig, len(configs))
		copy(profileConfigs, configs)
		profiles = append(profiles, ProfileTaskConfigs{Profile: name, Current: current, Configs: profileConfigs})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Profile < profiles[j].Profile })
	return profiles, nil
}

func (registry *TaskConfigRegistry) Select(profileName, configName string) error {
	configPaths, err := registry.discover()
	if err != nil {
		return err
	}
	configPath, ok := configPaths[configName]
	if !ok {
		return fmt.Errorf("bundled task config %q not found", configName)
	}
	config, err := NewConfig(&configPath)
	if err != nil {
		return fmt.Errorf("load bundled task config %q: %w", configName, err)
	}

	registry.mu.RLock()
	watcher := registry.watchers[profileName]
	registry.mu.RUnlock()
	if watcher == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}

	if err := registry.store.RecordTaskConfigSelection(profileName, configName); err != nil {
		return err
	}
	watcher.setConfig(config, configPath, configName)
	return nil
}

func (registry *TaskConfigRegistry) discover() (map[string]string, error) {
	configs := make(map[string]string)
	err := filepath.WalkDir(registry.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "tasks.yaml" {
			return nil
		}
		relative, err := filepath.Rel(registry.root, filepath.Dir(path))
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if name == "." || strings.HasPrefix(name, "../") {
			return nil
		}
		configs[name] = path
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover bundled task configs: %w", err)
	}
	return configs, nil
}

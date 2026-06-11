package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	mediaTypeImage = "image"
	mediaTypeVideo = "video"
)

var videoExtensions = map[string]struct{}{
	"3gp": {}, "3gpp": {}, "avi": {}, "flv": {}, "insv": {}, "m2t": {}, "m2ts": {},
	"m4v": {}, "mkv": {}, "mov": {}, "mp4": {}, "mpe": {}, "mpeg": {}, "mpg": {},
	"mts": {}, "ts": {}, "webm": {}, "wmv": {},
}

type TaskConfigOption struct {
	Name     string     `json:"name"`
	LastUsed *time.Time `json:"last_used,omitempty"`
}

type ProfileTaskConfigs struct {
	Profile      string             `json:"profile"`
	ImageCurrent string             `json:"image_current"`
	VideoCurrent string             `json:"video_current"`
	ImageConfigs []TaskConfigOption `json:"image_configs"`
	VideoConfigs []TaskConfigOption `json:"video_configs"`
	UseNvidia    bool               `json:"use_nvidia"`
	ImageScore   int                `json:"image_score"`
	VideoScore   int                `json:"video_score"`
	VideoCRF     int                `json:"video_crf"`
}

type discoveredTaskConfig struct {
	Name      string
	Path      string
	Config    *Config
	HasImages bool
	HasVideos bool
}

type TaskConfigRegistry struct {
	bundledRoot string
	customRoot  string
	store       *StatsStore
	mu          sync.RWMutex
	watchers    map[string]*FileWatcher
}

func NewTaskConfigRegistry(bundledRoot, customRoot string, store *StatsStore) *TaskConfigRegistry {
	return &TaskConfigRegistry{
		bundledRoot: filepath.Clean(bundledRoot),
		customRoot:  filepath.Clean(customRoot),
		store:       store,
		watchers:    make(map[string]*FileWatcher),
	}
}

func (registry *TaskConfigRegistry) Register(watcher *FileWatcher) error {
	registry.mu.Lock()
	registry.watchers[watcher.profile.Name] = watcher
	registry.mu.Unlock()

	configs, err := registry.discover()
	if err != nil {
		return err
	}
	legacySelection, err := registry.store.SelectedTaskConfig(watcher.profile.Name)
	if err != nil {
		return err
	}
	if legacySelection == "" {
		legacySelection = watcher.profile.LegacyTaskConfigName
	}
	migratedNvidia := legacySelection == "perceptual/perceptual-hevc-webp-nvidia"
	for _, mediaType := range []string{mediaTypeImage, mediaTypeVideo} {
		selected, err := registry.store.SelectedMediaTaskConfig(watcher.profile.Name, mediaType)
		if err != nil {
			return err
		}
		if selected == "" {
			selected = legacySelection
		}
		if selected == "perceptual/perceptual-hevc-webp-nvidia" {
			migratedNvidia = true
		}
		selected = migratedPerceptualConfigName(selected, mediaType)
		if selected == "" {
			selected = configNameForPath(configs, watcher.profile.ConfigFile)
		}
		if selected == "" {
			continue
		}
		config, ok := configs[selected]
		if !ok || !config.supports(mediaType) {
			continue
		}
		watcher.setConfig(mediaType, config.Config, config.Path, config.Name)
	}
	useNvidia, err := registry.store.ProfileNvidia(watcher.profile.Name)
	if err != nil {
		return err
	}
	if migratedNvidia {
		useNvidia = true
	}
	if watcher.profile.LegacyUseNvidia {
		useNvidia = true
	}
	watcher.setNvidiaEnabled(useNvidia)
	imageScore, err := registry.store.ProfileImageScore(watcher.profile.Name)
	if err != nil {
		return err
	}
	watcher.setImageScore(imageScore)
	videoScore, err := registry.store.ProfileVideoScore(watcher.profile.Name)
	if err != nil {
		return err
	}
	watcher.setVideoScore(videoScore)
	videoCRF, err := registry.store.ProfileVideoCRF(watcher.profile.Name)
	if err != nil {
		return err
	}
	watcher.setVideoCRF(videoCRF)
	return nil
}

func migratedPerceptualConfigName(name, mediaType string) string {
	switch name {
	case "perceptual/perceptual-av1":
		if mediaType == mediaTypeImage {
			return "perceptual/avif"
		}
		return "perceptual/av1"
	case "perceptual/perceptual-hevc-webp", "perceptual/perceptual-hevc-webp-nvidia":
		if mediaType == mediaTypeImage {
			return "perceptual/webp"
		}
		return "perceptual/hevc"
	default:
		return name
	}
}

func (registry *TaskConfigRegistry) List() ([]ProfileTaskConfigs, error) {
	configs, err := registry.discover()
	if err != nil {
		return nil, err
	}
	imageConfigs, err := registry.options(configs, mediaTypeImage)
	if err != nil {
		return nil, err
	}
	videoConfigs, err := registry.options(configs, mediaTypeVideo)
	if err != nil {
		return nil, err
	}

	registry.mu.RLock()
	defer registry.mu.RUnlock()
	profiles := make([]ProfileTaskConfigs, 0, len(registry.watchers))
	for name, watcher := range registry.watchers {
		profiles = append(profiles, ProfileTaskConfigs{
			Profile:      name,
			ImageCurrent: watcher.currentConfigName(mediaTypeImage),
			VideoCurrent: watcher.currentConfigName(mediaTypeVideo),
			ImageConfigs: append([]TaskConfigOption(nil), imageConfigs...),
			VideoConfigs: append([]TaskConfigOption(nil), videoConfigs...),
			UseNvidia:    watcher.nvidiaEnabled(),
			ImageScore:   watcher.currentImageScore(),
			VideoScore:   watcher.currentVideoScore(),
			VideoCRF:     watcher.currentVideoCRF(),
		})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Profile < profiles[j].Profile })
	return profiles, nil
}

func (registry *TaskConfigRegistry) SetNvidia(profileName string, enabled bool) error {
	registry.mu.RLock()
	watcher := registry.watchers[profileName]
	registry.mu.RUnlock()
	if watcher == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}
	if err := registry.store.SetProfileNvidia(profileName, enabled); err != nil {
		return err
	}
	watcher.setNvidiaEnabled(enabled)
	return nil
}

func (registry *TaskConfigRegistry) SetImageScore(profileName string, score int) error {
	if score < 0 || score > 100 {
		return fmt.Errorf("image score must be between 0 and 100")
	}
	registry.mu.RLock()
	watcher := registry.watchers[profileName]
	registry.mu.RUnlock()
	if watcher == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}
	if err := registry.store.SetProfileImageScore(profileName, score); err != nil {
		return err
	}
	watcher.setImageScore(score)
	return nil
}

func (registry *TaskConfigRegistry) SetVideoScore(profileName string, score int) error {
	if score < 0 || score > 100 {
		return fmt.Errorf("video score must be between 0 and 100")
	}
	registry.mu.RLock()
	watcher := registry.watchers[profileName]
	registry.mu.RUnlock()
	if watcher == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}
	if err := registry.store.SetProfileVideoScore(profileName, score); err != nil {
		return err
	}
	watcher.setVideoScore(score)
	return nil
}

func (registry *TaskConfigRegistry) SetVideoCRF(profileName string, crf int) error {
	if crf < 0 || crf > 63 {
		return fmt.Errorf("video CRF must be between 0 and 63")
	}
	registry.mu.RLock()
	watcher := registry.watchers[profileName]
	registry.mu.RUnlock()
	if watcher == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}
	if err := registry.store.SetProfileVideoCRF(profileName, crf); err != nil {
		return err
	}
	watcher.setVideoCRF(crf)
	return nil
}

func (registry *TaskConfigRegistry) Select(profileName, mediaType, configName string) error {
	if mediaType != mediaTypeImage && mediaType != mediaTypeVideo {
		return fmt.Errorf("media type must be image or video")
	}
	configs, err := registry.discover()
	if err != nil {
		return err
	}
	config, ok := configs[configName]
	if !ok {
		return fmt.Errorf("task config %q not found", configName)
	}
	if !config.supports(mediaType) {
		return fmt.Errorf("task config %q does not contain %s tasks", configName, mediaType)
	}

	registry.mu.RLock()
	watcher := registry.watchers[profileName]
	registry.mu.RUnlock()
	if watcher == nil {
		return fmt.Errorf("profile %q not found", profileName)
	}
	if watcher.currentConfigName(mediaType) == configName {
		return nil
	}

	if err := registry.store.RecordMediaTaskConfigSelection(profileName, mediaType, configName); err != nil {
		return err
	}
	watcher.setConfig(mediaType, config.Config, config.Path, config.Name)
	return nil
}

func (registry *TaskConfigRegistry) options(configs map[string]discoveredTaskConfig, mediaType string) ([]TaskConfigOption, error) {
	usage, err := registry.store.MediaTaskConfigUsage(mediaType)
	if err != nil {
		return nil, err
	}
	options := make([]TaskConfigOption, 0, len(configs))
	for name, config := range configs {
		if !config.supports(mediaType) {
			continue
		}
		option := TaskConfigOption{Name: name}
		if used, ok := usage[name]; ok {
			usedCopy := used
			option.LastUsed = &usedCopy
		}
		options = append(options, option)
	}
	sort.Slice(options, func(i, j int) bool {
		a, b := options[i], options[j]
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
	return options, nil
}

func (registry *TaskConfigRegistry) discover() (map[string]discoveredTaskConfig, error) {
	paths := make(map[string]string)
	if err := discoverTaskConfigPaths(registry.bundledRoot, "", paths); err != nil {
		return nil, fmt.Errorf("discover bundled task configs: %w", err)
	}
	if err := discoverTaskConfigPaths(registry.customRoot, "custom", paths); err != nil {
		return nil, fmt.Errorf("discover custom task configs: %w", err)
	}
	configs := make(map[string]discoveredTaskConfig, len(paths))
	for name, path := range paths {
		config, err := NewConfig(&path)
		if err != nil {
			return nil, fmt.Errorf("load task config %q: %w", name, err)
		}
		hasImages, hasVideos := configMediaTypes(config)
		configs[name] = discoveredTaskConfig{Name: name, Path: path, Config: config, HasImages: hasImages, HasVideos: hasVideos}
	}
	return configs, nil
}

func (config discoveredTaskConfig) supports(mediaType string) bool {
	if mediaType == mediaTypeVideo {
		return config.HasVideos
	}
	return config.HasImages
}

func configMediaTypes(config *Config) (bool, bool) {
	var hasImages, hasVideos bool
	for _, task := range config.Tasks {
		for _, extension := range task.Extensions {
			if mediaTypeForExtension(extension) == mediaTypeVideo {
				hasVideos = true
			} else {
				hasImages = true
			}
		}
	}
	return hasImages, hasVideos
}

func mediaTypeForExtension(extension string) string {
	if _, ok := videoExtensions[normalizeExtension(extension)]; ok {
		return mediaTypeVideo
	}
	return mediaTypeImage
}

func configNameForPath(configs map[string]discoveredTaskConfig, configPath string) string {
	for name, config := range configs {
		if filepath.Clean(config.Path) == filepath.Clean(configPath) {
			return name
		}
	}
	return ""
}

func discoverTaskConfigPaths(root, prefix string, configs map[string]string) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "tasks.yaml" {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if name == "." || strings.HasPrefix(name, "../") {
			return nil
		}
		if prefix != "" {
			name = prefix + "/" + name
		}
		configs[name] = path
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

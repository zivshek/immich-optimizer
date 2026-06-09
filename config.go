package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/viper"
)

type Task struct {
	Name            string   `mapstructure:"name"`
	Extensions      []string `mapstructure:"extensions"`
	Command         string   `mapstructure:"command"`
	CommandTemplate *template.Template
}

func (task *Task) Init() (err error) {
	values := map[string]string{
		"folder":    "/folder",
		"name":      "name",
		"extension": "ext",
	}

	task.CommandTemplate, err = template.New("command").Parse(task.Command)
	if err != nil {
		err = fmt.Errorf("task %s unable to parse command: %v", task.Name, err)
		return
	}

	var cmdLine bytes.Buffer
	err = task.CommandTemplate.Execute(&cmdLine, values)
	if err != nil {
		err = fmt.Errorf("task %s unable to execute template for command: %v", task.Name, err)
		return
	}

	return
}

type Config struct {
	Tasks []Task `mapstructure:"tasks"`
}

func NewConfig(configFile *string) (*Config, error) {
	var c *Config
	configViper := viper.New()
	configViper.SetConfigFile(*configFile)

	if err := configViper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	if err := configViper.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	for i := range c.Tasks {
		if err := c.Tasks[i].Init(); err != nil {
			return nil, fmt.Errorf("error validating config: %w", err)
		}
	}

	return c, nil
}

type ProfilesConfig struct {
	Profiles []ProfileConfig `mapstructure:"profiles"`
}

type ProfileConfig struct {
	Name                 string  `mapstructure:"name"`
	User                 string  `mapstructure:"user"`
	ImmichURL            string  `mapstructure:"immich_url"`
	ImmichAPIKey         string  `mapstructure:"immich_api_key"`
	APIKey               string  `mapstructure:"api_key"`
	WatchDir             string  `mapstructure:"watch_dir"`
	UndoneDir            string  `mapstructure:"undone_dir"`
	ConfigFile           string  `mapstructure:"tasks_file"`
	Tasks                *Config `mapstructure:"-"`
	LegacyTaskConfigName string  `mapstructure:"-"`
	LegacyUseNvidia      bool    `mapstructure:"-"`
}

func NewProfilesConfig(configFile string) (*ProfilesConfig, error) {
	profilesViper := viper.New()
	profilesViper.SetConfigFile(configFile)

	if err := profilesViper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading profiles file: %w", err)
	}

	var profiles ProfilesConfig
	if err := profilesViper.Unmarshal(&profiles); err != nil {
		return nil, fmt.Errorf("error unmarshaling profiles: %w", err)
	}
	if len(profiles.Profiles) == 0 {
		return nil, fmt.Errorf("profiles file must define at least one profile")
	}

	baseDir := filepath.Dir(configFile)
	for i := range profiles.Profiles {
		profile := &profiles.Profiles[i]
		profile.normalizeAliases()
		profile.expandEnvironment()
		profile.resolvePaths(baseDir)
	}

	if err := profiles.validate(); err != nil {
		return nil, err
	}
	return &profiles, nil
}

func NewProfilesFromEnvironment(profileNames, immichURL, tasksFile string) (*ProfilesConfig, error) {
	names := strings.Split(profileNames, ",")
	profiles := &ProfilesConfig{
		Profiles: make([]ProfileConfig, 0, len(names)),
	}

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		envPrefix := "IUO_PROFILE_" + normalizeProfileEnvName(name) + "_"
		profiles.Profiles = append(profiles.Profiles, ProfileConfig{
			Name:         name,
			ImmichURL:    immichURL,
			ImmichAPIKey: os.Getenv(envPrefix + "API_KEY"),
			WatchDir:     os.Getenv(envPrefix + "WATCH_DIR"),
			UndoneDir:    os.Getenv(envPrefix + "UNDONE_DIR"),
			ConfigFile:   tasksFile,
		})
	}

	if len(profiles.Profiles) == 0 {
		return nil, fmt.Errorf("IUO_PROFILES must define at least one profile name")
	}
	if err := profiles.validate(); err != nil {
		return nil, err
	}
	return profiles, nil
}

func NewProfilesFromInlineConfig(config, immichURL, tasksFile string) (*ProfilesConfig, error) {
	profilesViper := viper.New()
	profilesViper.SetConfigType("yaml")
	if err := profilesViper.ReadConfig(strings.NewReader(config)); err != nil {
		return nil, fmt.Errorf("error reading IUO_PROFILES_CONFIG: %w", err)
	}

	var profiles ProfilesConfig
	if err := profilesViper.Unmarshal(&profiles); err != nil {
		return nil, fmt.Errorf("error unmarshaling IUO_PROFILES_CONFIG: %w", err)
	}
	if len(profiles.Profiles) == 0 {
		return nil, fmt.Errorf("IUO_PROFILES_CONFIG must define at least one profile")
	}

	for i := range profiles.Profiles {
		profile := &profiles.Profiles[i]
		profile.normalizeAliases()
		profile.expandEnvironment()
		if profile.ImmichURL == "" {
			profile.ImmichURL = immichURL
		}
		if profile.ConfigFile == "" {
			profile.ConfigFile = tasksFile
		}
	}

	if err := profiles.validate(); err != nil {
		return nil, err
	}
	return &profiles, nil
}

func (profiles *ProfilesConfig) validate() error {
	names := make(map[string]struct{}, len(profiles.Profiles))
	watchDirs := make(map[string]string, len(profiles.Profiles))
	for i := range profiles.Profiles {
		profile := &profiles.Profiles[i]
		if err := profile.validate(); err != nil {
			return fmt.Errorf("invalid profile %q: %w", profile.Name, err)
		}
		if _, exists := names[profile.Name]; exists {
			return fmt.Errorf("profile name %q is duplicated", profile.Name)
		}
		names[profile.Name] = struct{}{}

		cleanWatchDir := filepath.Clean(profile.WatchDir)
		for name, otherWatchDir := range watchDirs {
			if pathsOverlap(cleanWatchDir, otherWatchDir) {
				return fmt.Errorf("watch directory for profile %q overlaps profile %q", profile.Name, name)
			}
		}
		watchDirs[profile.Name] = cleanWatchDir
	}
	return nil
}

func normalizeProfileEnvName(name string) string {
	var normalized strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' {
			char -= 'a' - 'A'
		}
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			normalized.WriteRune(char)
		} else {
			normalized.WriteByte('_')
		}
	}
	return normalized.String()
}

func (profile *ProfileConfig) expandEnvironment() {
	profile.ImmichURL = os.ExpandEnv(profile.ImmichURL)
	profile.ImmichAPIKey = os.ExpandEnv(profile.ImmichAPIKey)
	profile.WatchDir = os.ExpandEnv(profile.WatchDir)
	profile.UndoneDir = os.ExpandEnv(profile.UndoneDir)
	profile.ConfigFile = os.ExpandEnv(profile.ConfigFile)
}

func (profile *ProfileConfig) normalizeAliases() {
	if profile.Name == "" {
		profile.Name = profile.User
	}
	if profile.ImmichAPIKey == "" {
		profile.ImmichAPIKey = profile.APIKey
	}
}

func (profile *ProfileConfig) resolvePaths(baseDir string) {
	profile.WatchDir = resolveRelativePath(baseDir, profile.WatchDir)
	profile.UndoneDir = resolveRelativePath(baseDir, profile.UndoneDir)
	profile.ConfigFile = resolveRelativePath(baseDir, profile.ConfigFile)
}

func resolveRelativePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}

func pathsOverlap(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

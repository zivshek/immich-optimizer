package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/viper"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type AppConfig struct {
	ShowVersion           bool
	ImmichURL             string
	ImmichAPIKey          string
	WatchDir              string
	UndoneDir             string
	ConfigFile            string
	ProfileNames          string
	ProfilesInlineConfig  string
	ProfilesFile          string
	DashboardAddress      string
	StatsDatabase         string
	BundledConfigsDir     string
	CustomProfilesDir     string
	MaxConcurrentRequests int
	HTTPTimeoutSeconds    int
	InotifyBufferSize     int
	Semaphore             chan struct{}
	Tasks                 *Config
	Profiles              []ProfileConfig
}

func NewAppConfig() *AppConfig {
	maxConcurrent := 10
	return &AppConfig{
		MaxConcurrentRequests: maxConcurrent,
		HTTPTimeoutSeconds:    120,
		InotifyBufferSize:     8192, // 8KB buffer for better performance
		Semaphore:             make(chan struct{}, maxConcurrent),
	}
}

func loadAppConfig() (*AppConfig, error) {
	appConfig := NewAppConfig()

	viper.SetEnvPrefix("iuo")
	viper.AutomaticEnv()
	viper.BindEnv("immich_url")
	viper.BindEnv("immich_api_key")
	viper.BindEnv("watch_dir")
	viper.BindEnv("undone_dir")
	viper.BindEnv("tasks_file")
	viper.BindEnv("profiles_file")
	viper.BindEnv("profiles")
	viper.BindEnv("profiles_config")
	viper.BindEnv("dashboard_address")
	viper.BindEnv("stats_database")
	viper.BindEnv("bundled_configs_dir")
	viper.BindEnv("custom_profiles_dir")

	viper.SetDefault("immich_url", "")
	viper.SetDefault("immich_api_key", "")
	viper.SetDefault("watch_dir", "/watch")
	viper.SetDefault("undone_dir", "/undone")
	viper.SetDefault("tasks_file", "tasks.yaml")
	viper.SetDefault("profiles_file", "")
	viper.SetDefault("profiles", "")
	viper.SetDefault("profiles_config", "")
	viper.SetDefault("dashboard_address", ":8098")
	viper.SetDefault("stats_database", "/data/immich-optimizer.db")
	viper.SetDefault("bundled_configs_dir", "/etc/immich-optimizer/bundled-configs")
	viper.SetDefault("custom_profiles_dir", "/custom_profiles")

	flag.BoolVar(&appConfig.ShowVersion, "version", false, "Show the current version")
	flag.StringVar(&appConfig.ImmichURL, "immich_url", viper.GetString("immich_url"), "Immich server URL. Example: http://immich-server:2283")
	flag.StringVar(&appConfig.ImmichAPIKey, "immich_api_key", viper.GetString("immich_api_key"), "Immich API key")
	flag.StringVar(&appConfig.WatchDir, "watch_dir", viper.GetString("watch_dir"), "Directory to watch for new files")
	flag.StringVar(&appConfig.UndoneDir, "undone_dir", viper.GetString("undone_dir"), "Directory to copy files that failed processing or upload")
	flag.StringVar(&appConfig.ConfigFile, "tasks_file", viper.GetString("tasks_file"), "Path to the configuration file")
	flag.StringVar(&appConfig.ProfilesFile, "profiles_file", viper.GetString("profiles_file"), "Path to a multi-user profiles configuration file")
	flag.StringVar(&appConfig.ProfileNames, "profiles", viper.GetString("profiles"), "Comma-separated profile names configured through environment variables")
	flag.StringVar(&appConfig.ProfilesInlineConfig, "profiles_config", viper.GetString("profiles_config"), "Inline YAML multi-user profiles configuration")
	flag.StringVar(&appConfig.DashboardAddress, "dashboard_address", viper.GetString("dashboard_address"), "Dashboard listen address")
	flag.StringVar(&appConfig.StatsDatabase, "stats_database", viper.GetString("stats_database"), "SQLite statistics database path")
	flag.StringVar(&appConfig.BundledConfigsDir, "bundled_configs_dir", viper.GetString("bundled_configs_dir"), "Directory containing bundled task configurations")
	flag.StringVar(&appConfig.CustomProfilesDir, "custom_profiles_dir", viper.GetString("custom_profiles_dir"), "Directory containing custom task configurations")
	flag.Parse()

	if appConfig.ShowVersion {
		return appConfig, nil
	}
	if err := appConfig.validate(); err != nil {
		return nil, err
	}
	return appConfig, nil
}

func (ac *AppConfig) validate() error {
	if ac.ProfilesFile != "" {
		profiles, err := NewProfilesConfig(ac.ProfilesFile)
		if err != nil {
			return err
		}
		for i := range profiles.Profiles {
			if err := ac.prepareProfile(&profiles.Profiles[i]); err != nil {
				return fmt.Errorf("invalid profile %q: %w", profiles.Profiles[i].Name, err)
			}
		}
		ac.Profiles = profiles.Profiles
		return nil
	}
	if ac.ProfilesInlineConfig != "" {
		profiles, err := NewProfilesFromInlineConfig(ac.ProfilesInlineConfig, ac.ImmichURL, ac.ConfigFile)
		if err != nil {
			return err
		}
		for i := range profiles.Profiles {
			if err := ac.prepareProfile(&profiles.Profiles[i]); err != nil {
				return fmt.Errorf("invalid profile %q: %w", profiles.Profiles[i].Name, err)
			}
		}
		ac.Profiles = profiles.Profiles
		return nil
	}
	if ac.ProfileNames != "" {
		profiles, err := NewProfilesFromEnvironment(ac.ProfileNames, ac.ImmichURL, ac.ConfigFile)
		if err != nil {
			return err
		}
		for i := range profiles.Profiles {
			if err := ac.prepareProfile(&profiles.Profiles[i]); err != nil {
				return fmt.Errorf("invalid profile %q: %w", profiles.Profiles[i].Name, err)
			}
		}
		ac.Profiles = profiles.Profiles
		return nil
	}

	profile := ProfileConfig{
		Name:         "default",
		ImmichURL:    ac.ImmichURL,
		ImmichAPIKey: ac.ImmichAPIKey,
		WatchDir:     ac.WatchDir,
		UndoneDir:    ac.UndoneDir,
		ConfigFile:   ac.ConfigFile,
	}
	if err := ac.prepareProfile(&profile); err != nil {
		return err
	}
	ac.Tasks = profile.Tasks
	ac.Profiles = []ProfileConfig{profile}
	return nil
}

func (ac *AppConfig) prepareProfile(profile *ProfileConfig) error {
	if err := profile.validate(); err != nil {
		return err
	}

	if mkdirErr := os.MkdirAll(profile.WatchDir, 0750); mkdirErr != nil {
		return fmt.Errorf("error creating watch directory: %v", mkdirErr)
	}
	if mkdirErr := os.MkdirAll(profile.UndoneDir, 0750); mkdirErr != nil {
		return fmt.Errorf("error creating undone directory: %v", mkdirErr)
	}

	var err error
	profile.Tasks, err = NewConfig(&profile.ConfigFile)
	if err != nil {
		return fmt.Errorf("error loading tasks file: %v", err)
	}
	return nil
}

func (profile *ProfileConfig) validate() error {
	if strings.TrimSpace(profile.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if profile.ImmichURL == "" {
		return fmt.Errorf("immich_url is required")
	}

	parsedURL, urlErr := url.Parse(profile.ImmichURL)
	if urlErr != nil {
		return fmt.Errorf("invalid immich_url format: %w", urlErr)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("immich_url must use http or https scheme")
	}
	if parsedURL.Host == "" {
		return fmt.Errorf("immich_url must include a valid host")
	}
	if len(strings.TrimSpace(profile.ImmichAPIKey)) < 10 {
		return fmt.Errorf("immich_api_key appears to be too short (minimum 10 characters)")
	}
	if profile.WatchDir == "" {
		return fmt.Errorf("watch_dir is required")
	}
	if profile.UndoneDir == "" {
		return fmt.Errorf("undone_dir is required")
	}
	if profile.ConfigFile == "" {
		return fmt.Errorf("tasks_file is required")
	}
	return nil
}

func main() {
	config, err := loadAppConfig()
	if err != nil {
		log.Fatal(err)
	}
	if config.ShowVersion {
		fmt.Println(printVersion())
		return
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logBuffer := NewLogBuffer(500)
	logOutput := io.MultiWriter(os.Stdout, logBuffer)
	baseLogger := log.New(logOutput, "", log.Ldate|log.Ltime)
	customLogger := newCustomLogger(baseLogger, "")
	customLogger.Printf("Starting %s", printVersion())

	statsStore, err := NewStatsStore(config.StatsDatabase)
	if err != nil {
		customLogger.Printf("Error opening statistics database: %v", err)
		os.Exit(1)
	}
	defer statsStore.Close()

	configRegistry := NewTaskConfigRegistry(config.BundledConfigsDir, config.CustomProfilesDir, statsStore)

	var watchers []*FileWatcher
	for i := range config.Profiles {
		profile := &config.Profiles[i]
		profileBaseLogger := log.New(logOutput, fmt.Sprintf("profile %s: ", profile.Name), log.Ldate|log.Ltime)
		profileLogger := newCustomLogger(profileBaseLogger, "")
		immichClient := NewImmichClient(profile.ImmichURL, profile.ImmichAPIKey, config.HTTPTimeoutSeconds, profileLogger)
		watcher, err := NewFileWatcher(profile, immichClient, profileBaseLogger, config.InotifyBufferSize, config.Semaphore, statsStore)
		if err != nil {
			customLogger.Printf("Error creating watcher for profile %s: %v", profile.Name, err)
			stopWatchers(watchers)
			os.Exit(1)
		}
		if err := configRegistry.Register(watcher); err != nil {
			customLogger.Printf("Error restoring task config for profile %s: %v", profile.Name, err)
		}
		if err := watcher.Start(); err != nil {
			customLogger.Printf("Error starting watcher for profile %s: %v", profile.Name, err)
			watcher.Stop()
			stopWatchers(watchers)
			os.Exit(1)
		}
		watchers = append(watchers, watcher)
	}
	defer stopWatchers(watchers)

	dashboard := NewDashboardServer(config.DashboardAddress, statsStore, configRegistry, logBuffer, logOutput)
	dashboard.Start()
	defer dashboard.Close()

	// Block until we receive our signal
	<-sigChan

	customLogger.Printf("Shutting down gracefully...")

	// Create a deadline to wait for
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Stop the watcher gracefully
	done := make(chan struct{})
	go func() {
		stopWatchers(watchers)
		close(done)
	}()

	select {
	case <-done:
		customLogger.Printf("Shutdown completed successfully")
	case <-shutdownCtx.Done():
		customLogger.Printf("Shutdown timeout exceeded, forcing exit")
	}
}

func stopWatchers(watchers []*FileWatcher) {
	for _, watcher := range watchers {
		watcher.Stop()
	}
}

func printVersion() string {
	return fmt.Sprintf("immich-optimizer %s, commit %s, built at %s", version, commit, date)
}

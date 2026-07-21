package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultBufferSize = 4096
	inotifyWatchMask  = unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_CREATE
)

// FileWatcher monitors directory changes using inotify and processes files
type FileWatcher struct {
	fd             int            // inotify file descriptor
	watchDir       string         // root directory to watch
	immichClient   *ImmichClient  // client for uploading to Immich
	config         *Config        // startup fallback processing configuration
	logger         *log.Logger    // logger instance
	watchMap       map[string]int // maps directory paths to watch descriptors
	bufferSize     int            // buffer size for reading inotify events
	profile        *ProfileConfig // profile-specific directories and tasks
	statsStore     *StatsStore
	configMu       sync.RWMutex
	imageConfig    taskConfigSelection
	videoConfig    taskConfigSelection
	useNvidia      bool
	dropAPAC       bool
	imageScore     int
	videoScore     int
	videoCRF       int
	semaphore      chan struct{} // shared application concurrency limit
	stopOnce       sync.Once
	processing     sync.Map // tracks files actively being processed to avoid duplicate concurrent tasks
	closedInodes   sync.Map // tracks inodes of recently closed temporary files
	pendingCreates sync.Map // tracks inodes of recently created non-temp files waiting for IN_CLOSE_WRITE
}

// pendingCreate stores an IN_CREATE event waiting for an IN_CLOSE_WRITE
type pendingCreate struct {
	path string
	ts   time.Time
}

// NewFileWatcher creates a new file watcher instance
func NewFileWatcher(profile *ProfileConfig, immichClient *ImmichClient, logger *log.Logger, bufferSize int, semaphore chan struct{}, statsStore *StatsStore) (*FileWatcher, error) {
	fd, err := unix.InotifyInit()
	if err != nil {
		return nil, fmt.Errorf("failed to create inotify instance: %w", err)
	}

	fw := &FileWatcher{
		fd:           fd,
		watchDir:     profile.WatchDir,
		immichClient: immichClient,
		config:       profile.Tasks,
		logger:       logger,
		watchMap:     make(map[string]int),
		bufferSize:   bufferSize,
		profile:      profile,
		semaphore:    semaphore,
		statsStore:   statsStore,
		imageConfig:  taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
		videoConfig:  taskConfigSelection{Config: profile.Tasks, File: profile.ConfigFile, Name: profile.ConfigFile},
		imageScore:   85,
		videoScore:   95,
		videoCRF:     28,
	}

	return fw, nil
}

type taskConfigSelection struct {
	Config *Config
	File   string
	Name   string
}

func (fw *FileWatcher) activeConfig(mediaType string) taskConfigSelection {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	var selection taskConfigSelection
	if mediaType == mediaTypeVideo {
		selection = fw.videoConfig
	} else {
		selection = fw.imageConfig
	}
	if selection.Config == nil {
		selection.Config = fw.config
		if fw.profile != nil {
			selection.File = fw.profile.ConfigFile
			selection.Name = fw.profile.ConfigFile
		}
	}
	return selection
}

func (fw *FileWatcher) currentConfigName(mediaType string) string {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	if mediaType == mediaTypeVideo {
		return fw.videoConfig.Name
	}
	return fw.imageConfig.Name
}

func (fw *FileWatcher) setConfig(mediaType string, config *Config, configFile, configName string) {
	fw.configMu.Lock()
	defer fw.configMu.Unlock()
	selection := taskConfigSelection{Config: config, File: configFile, Name: configName}
	if mediaType == mediaTypeVideo {
		fw.videoConfig = selection
	} else {
		fw.imageConfig = selection
	}
	fw.logger.Printf("Profile %s: selected %s task config %s", fw.profile.Name, mediaType, configName)
}

func (fw *FileWatcher) nvidiaEnabled() bool {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	return fw.useNvidia
}

func (fw *FileWatcher) setNvidiaEnabled(enabled bool) {
	fw.configMu.Lock()
	defer fw.configMu.Unlock()
	fw.useNvidia = enabled
	fw.logger.Printf("Profile %s: NVIDIA processing %s", fw.profile.Name, map[bool]string{true: "enabled", false: "disabled"}[enabled])
}

func (fw *FileWatcher) dropAPACEnabled() bool {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	return fw.dropAPAC
}

func (fw *FileWatcher) setDropAPACEnabled(enabled bool) {
	fw.configMu.Lock()
	defer fw.configMu.Unlock()
	fw.dropAPAC = enabled
	fw.logger.Printf("Profile %s: APAC spatial audio %s", fw.profile.Name, map[bool]string{true: "will be dropped when present", false: "will preserve by uploading originals"}[enabled])
}

func (fw *FileWatcher) currentImageScore() int {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	if fw.imageScore <= 0 {
		return 85
	}
	return fw.imageScore
}

func (fw *FileWatcher) setImageScore(score int) {
	fw.configMu.Lock()
	defer fw.configMu.Unlock()
	fw.imageScore = score
	fw.logger.Printf("Profile %s: image SSIMULACRA2 target set to %d", fw.profile.Name, score)
}

func (fw *FileWatcher) currentVideoScore() int {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	if fw.videoScore <= 0 {
		return 95
	}
	return fw.videoScore
}

func (fw *FileWatcher) setVideoScore(score int) {
	fw.configMu.Lock()
	defer fw.configMu.Unlock()
	fw.videoScore = score
	fw.logger.Printf("Profile %s: video VMAF target set to %d", fw.profile.Name, score)
}

func (fw *FileWatcher) currentVideoCRF() int {
	fw.configMu.RLock()
	defer fw.configMu.RUnlock()
	if fw.videoCRF <= 0 {
		return 28
	}
	return fw.videoCRF
}

func (fw *FileWatcher) setVideoCRF(crf int) {
	fw.configMu.Lock()
	defer fw.configMu.Unlock()
	fw.videoCRF = crf
	fw.logger.Printf("Profile %s: video CRF set to %d", fw.profile.Name, crf)
}

// Start begins monitoring the directory for file changes
func (fw *FileWatcher) Start() error {
	fw.logger.Printf("Profile %s: starting recursive file watcher on directory: %s", fw.profile.Name, fw.watchDir)

	// Add watches recursively
	err := fw.addWatchRecursive(fw.watchDir)
	if err != nil {
		return fmt.Errorf("failed to add recursive watches: %w", err)
	}

	// Process existing files in all directories asynchronously
	go fw.processExistingFilesRecursive(fw.watchDir)

	// Start sweeping stale cached inodes
	go fw.cleanupLoop()

	// Start watching for new files
	go fw.watchLoop()

	return nil
}

// cleanupLoop periodically sweeps stale cached inodes to prevent memory leaks
func (fw *FileWatcher) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		fw.closedInodes.Range(func(key, value any) bool {
			if ts, ok := value.(time.Time); ok && now.Sub(ts) > time.Minute {
				fw.closedInodes.Delete(key)
			}
			return true
		})

		fw.pendingCreates.Range(func(key, value any) bool {
			if pc, ok := value.(pendingCreate); ok && now.Sub(pc.ts) > time.Minute {
				fw.pendingCreates.Delete(key)
			}
			return true
		})
	}
}

// Stop closes the file watcher and cleans up resources
func (fw *FileWatcher) Stop() {
	fw.stopOnce.Do(func() {
		for _, wd := range fw.watchMap {
			unix.InotifyRmWatch(fw.fd, uint32(wd))
		}
		unix.Close(fw.fd)
	})
}

// addWatchRecursive adds inotify watches to all directories recursively
func (fw *FileWatcher) addWatchRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path != dir && fw.isHiddenPath(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return fw.addDirectoryWatch(path)
		}

		return nil
	})
}

// addDirectoryWatch adds an inotify watch to a specific directory
func (fw *FileWatcher) addDirectoryWatch(path string) error {
	wd, err := unix.InotifyAddWatch(fw.fd, path, inotifyWatchMask)
	if err != nil {
		return fmt.Errorf("failed to add watch for %s: %w", path, err)
	}
	fw.watchMap[path] = wd
	fw.logger.Printf("Added watch for directory: %s", path)
	return nil
}

// processExistingFilesRecursive processes all existing files in the directory
func (fw *FileWatcher) processExistingFilesRecursive(dir string) {
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fw.logger.Printf("Error walking directory %s: %v", path, err)
			return nil
		}

		if path != dir && fw.isHiddenPath(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			go fw.processFile(path)
		}

		return nil
	})
}

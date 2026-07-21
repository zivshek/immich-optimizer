package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const imageMinimumReductionPercent = 15

var (
	fileReadyPollInterval   = 500 * time.Millisecond
	fileReadyStableDuration = 2 * time.Second
	fileReadyTimeout        = 2 * time.Minute
)

// isTempFile checks if a file is a known temporary file format used by sync clients
func (fw *FileWatcher) isTempFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".tmp", ".temp", ".part", ".crdownload", ".tacitpart", ".pending":
		return true
	}
	if strings.HasPrefix(ext, ".syncthing") {
		return true
	}
	return false
}

// isHiddenPath checks whether any path component below the watched root begins with a dot.
func (fw *FileWatcher) isHiddenPath(filePath string) bool {
	relativePath, err := filepath.Rel(fw.watchDir, filePath)
	if err != nil {
		return false
	}

	for _, component := range strings.Split(filepath.Clean(relativePath), string(filepath.Separator)) {
		if component != "." && strings.HasPrefix(component, ".") {
			return true
		}
	}
	return false
}

// processFile handles the complete file processing workflow
func (fw *FileWatcher) processFile(originalFilePath string) {
	// Deduplicate parallel events: abort if this file is already running in another goroutine
	if _, loaded := fw.processing.LoadOrStore(originalFilePath, true); loaded {
		return
	}
	defer fw.processing.Delete(originalFilePath)

	if fw.isHiddenPath(originalFilePath) {
		return
	}

	if fw.semaphore != nil {
		fw.semaphore <- struct{}{}
		defer func() { <-fw.semaphore }()
	}

	if !fw.waitForReadyFile(originalFilePath) {
		return
	}

	if !fw.validateFile(originalFilePath) {
		return
	}

	if fw.isTempFile(originalFilePath) {
		return
	}

	fw.logger.Printf("Processing file: %s", originalFilePath)
	mediaType := mediaTypeForExtension(filepath.Ext(originalFilePath))
	active := fw.activeConfig(mediaType)

	if !fw.shouldOptimizeFile(originalFilePath, active.Config) {
		if fw.uploadToImmich(originalFilePath) {
			size := fileSize(originalFilePath)
			fw.recordProcessed(filepath.Base(originalFilePath), originalFilePath, size, size)
			fw.cleanupOriginalFile(originalFilePath)
		}
		return
	}

	tp, err := fw.createTaskProcessor(originalFilePath, active.File, fw.nvidiaEnabled(), fw.dropAPACEnabled(), fw.currentImageScore(), fw.currentVideoScore(), fw.currentVideoCRF())
	if err != nil {
		if fw.handleProcessingError(originalFilePath, err) {
			fw.cleanupOriginalFile(originalFilePath)
		}
		return
	}
	defer tp.Close()

	if err := tp.Process(active.Config.Tasks); err != nil {
		if fw.handleProcessingError(originalFilePath, err) {
			fw.cleanupOriginalFile(originalFilePath)
		}
		return
	}

	if fw.handleProcessingSuccess(originalFilePath, tp) {
		fw.cleanupOriginalFile(originalFilePath)
	}
}

// validateFile checks if the file exists and is not a directory
func (fw *FileWatcher) validateFile(filePath string) bool {
	info, err := os.Stat(filePath)
	if err != nil {
		fw.logger.Printf("Error getting file info for %s: %v", filePath, err)
		return false
	}
	return !info.IsDir()
}

func (fw *FileWatcher) waitForReadyFile(filePath string) bool {
	deadline := time.Now().Add(fileReadyTimeout)
	var lastSize int64 = -1
	var lastMod time.Time
	var stableSince time.Time
	var lastProbeErr error

	for {
		now := time.Now()
		info, err := os.Stat(filePath)
		if err != nil {
			fw.logger.Printf("Error getting file info for %s: %v", filePath, err)
			return false
		}
		if info.IsDir() {
			return false
		}

		size := info.Size()
		modTime := info.ModTime()
		if size > 0 && size == lastSize && modTime.Equal(lastMod) {
			if stableSince.IsZero() {
				stableSince = now
			}
			if fileReadyStableDuration <= 0 || now.Sub(stableSince) >= fileReadyStableDuration {
				if err := validateReadyMedia(filePath); err == nil {
					return true
				} else {
					lastProbeErr = err
				}
			}
		} else {
			lastSize = size
			lastMod = modTime
			stableSince = now
		}

		if !deadline.After(now) {
			if size <= 0 {
				fw.logger.Printf("Skipping file %s because it did not become non-empty before timeout", filePath)
			} else if lastProbeErr != nil {
				fw.logger.Printf("Skipping file %s because it did not become readable before timeout: %v", filePath, lastProbeErr)
			} else {
				fw.logger.Printf("Skipping file %s because it did not become stable before timeout", filePath)
			}
			return false
		}
		time.Sleep(fileReadyPollInterval)
	}
}

func validateReadyMedia(filePath string) error {
	if mediaTypeForExtension(filepath.Ext(filePath)) != mediaTypeVideo {
		return nil
	}
	return exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	).Run()
}

// shouldOptimizeFile determines if a file should be processed for optimization
func (fw *FileWatcher) shouldOptimizeFile(filePath string, config *Config) bool {
	if config == nil {
		fw.logger.Printf("Skipping optimization for %s (no task config selected)", filePath)
		return false
	}
	extension := filepath.Ext(filePath)
	if !shouldProcessExtension(extension, config.Tasks) {
		fw.logger.Printf("Skipping file %s (extension %s not configured for processing)", filePath, extension)
		return false
	}
	return true
}

// createTaskProcessor creates and configures a new task processor for the file
func (fw *FileWatcher) createTaskProcessor(filePath, configFile string, useNvidia, dropAPAC bool, imageScore, videoScore, videoCRF int) (*TaskProcessor, error) {
	tp, err := NewTaskProcessor(filePath)
	if err != nil {
		return nil, err
	}

	jobLogger := newCustomLogger(fw.logger, fmt.Sprintf("file %s: ", filePath))
	tp.SetLogger(jobLogger)

	if configFile != "" {
		tp.SetConfigDir(filepath.Dir(configFile))
	}
	if useNvidia {
		tp.SetEnvironment("IUO_USE_NVIDIA=1")
	} else {
		tp.SetEnvironment("IUO_USE_NVIDIA=0")
	}
	if dropAPAC {
		tp.SetEnvironment("IUO_DROP_APAC=1")
	} else {
		tp.SetEnvironment("IUO_DROP_APAC=0")
	}
	tp.SetEnvironment(fmt.Sprintf("IUO_IMAGE_SCORE=%d", imageScore))
	tp.SetEnvironment(fmt.Sprintf("IUO_VIDEO_SCORE=%d", videoScore))
	tp.SetEnvironment(fmt.Sprintf("IUO_VIDEO_CRF=%d", videoCRF))

	return tp, nil
}

// handleProcessingError falls back to uploading the original after optimization fails.
func (fw *FileWatcher) handleProcessingError(filePath string, err error) bool {
	fw.logger.Printf("Error processing file %s: %v", filePath, err)
	fw.logger.Printf("Uploading original file after optimization failure: %s", filePath)
	return fw.uploadOriginalFile(filePath)
}

// handleProcessingSuccess handles successful file processing and determines upload strategy
func (fw *FileWatcher) handleProcessingSuccess(originalFilePath string, tp *TaskProcessor) bool {
	if fw.shouldUploadProcessedFile(tp) {
		return fw.uploadProcessedFile(originalFilePath, tp)
	}
	return fw.uploadOriginalFile(originalFilePath)
}

// shouldUploadProcessedFile determines if the processed file should be uploaded instead of original
func (fw *FileWatcher) shouldUploadProcessedFile(tp *TaskProcessor) bool {
	if tp.ProcessedFile == nil || tp.ProcessedSize <= 0 || tp.OriginalSize <= tp.ProcessedSize {
		if tp.ProcessedFile != nil && tp.ProcessedSize > 0 {
			fw.logger.Printf(
				"Optimized output was not smaller (%s -> %s); uploading original file instead",
				humanReadableSize(tp.OriginalSize),
				humanReadableSize(tp.ProcessedSize),
			)
		}
		return false
	}
	if mediaTypeForExtension(tp.OriginalExtension) != mediaTypeImage {
		return true
	}
	savedBytes := tp.OriginalSize - tp.ProcessedSize
	if savedBytes*100 >= tp.OriginalSize*imageMinimumReductionPercent {
		return true
	}
	fw.logger.Printf(
		"Optimized image saved less than %d%% (%s -> %s); uploading original file instead",
		imageMinimumReductionPercent,
		humanReadableSize(tp.OriginalSize),
		humanReadableSize(tp.ProcessedSize),
	)
	return false
}

// uploadProcessedFile uploads the optimized version of the file
func (fw *FileWatcher) uploadProcessedFile(originalFilePath string, tp *TaskProcessor) bool {
	processedFilePath, err := tp.GetProcessedFilePath()
	if err != nil {
		fw.logger.Printf("Error getting processed file path: %v", err)
		return fw.uploadToImmich(originalFilePath)
	}

	fw.logger.Printf("Using optimized file: %s -> %s",
		humanReadableSize(tp.OriginalSize),
		humanReadableSize(tp.ProcessedSize))
	processedFilename := tp.ProcessedFilename
	if processedFilename == "" {
		processedFilename = filepath.Base(originalFilePath)
	}
	if !fw.uploadToImmichWithFilenameFromSource(processedFilePath, processedFilename, originalFilePath) {
		return false
	}
	fw.recordProcessed(processedFilename, originalFilePath, tp.OriginalSize, tp.ProcessedSize)
	return true
}

func (fw *FileWatcher) recordFailure(filename, originalFilePath string, jobError error) {
	if fw.statsStore == nil {
		return
	}
	profileName := ""
	if fw.profile != nil {
		profileName = fw.profile.Name
	}
	if err := fw.statsStore.RecordFailure(profileName, filename, mediaResolution(originalFilePath), fileSize(originalFilePath), jobError); err != nil {
		fw.logger.Printf("Error recording failed job for %s: %v", filename, err)
	}
}

// uploadOriginalFile uploads the original file without optimization
func (fw *FileWatcher) uploadOriginalFile(filePath string) bool {
	fw.logger.Printf("Original file uploaded (no optimization achieved)")
	if !fw.uploadToImmich(filePath) {
		return false
	}
	size := fileSize(filePath)
	fw.recordProcessed(filepath.Base(filePath), filePath, size, size)
	return true
}

// cleanupOriginalFile removes the original file after successful processing
func (fw *FileWatcher) cleanupOriginalFile(filePath string) {
	if err := os.Remove(filePath); err != nil {
		fw.logger.Printf("Error removing file %s after upload: %v", filePath, err)
	}
}

func (fw *FileWatcher) recordProcessed(filename, originalFilePath string, originalBytes, uploadedBytes int64) {
	if fw.statsStore == nil {
		return
	}
	resolution := mediaResolution(originalFilePath)
	if err := fw.statsStore.Record(fw.profile.Name, filename, resolution, originalBytes, uploadedBytes); err != nil {
		fw.logger.Printf("Error recording statistics for %s: %v", filename, err)
	}
}

func mediaResolution(filePath string) string {
	output, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:stream_side_data=rotation",
		"-of", "json",
		filePath,
	).Output()
	if err != nil {
		return ""
	}
	return parseMediaResolution(output)
}

func parseMediaResolution(output []byte) string {
	type sideData struct {
		Rotation float64 `json:"rotation"`
	}
	type stream struct {
		Width    int        `json:"width"`
		Height   int        `json:"height"`
		SideData []sideData `json:"side_data_list"`
	}
	type probeResult struct {
		Streams []stream `json:"streams"`
	}

	var probe probeResult
	if err := json.Unmarshal(output, &probe); err != nil || len(probe.Streams) == 0 {
		return ""
	}

	width := probe.Streams[0].Width
	height := probe.Streams[0].Height
	for _, sideData := range probe.Streams[0].SideData {
		rotation := math.Mod(math.Abs(sideData.Rotation), 180)
		if math.Abs(rotation-90) < 0.5 {
			width, height = height, width
			break
		}
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", width, height)
}

func fileSize(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0
	}
	return info.Size()
}

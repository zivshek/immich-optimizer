package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	if !fw.validateFile(originalFilePath) {
		return
	}

	if fw.isTempFile(originalFilePath) {
		return
	}

	fw.logger.Printf("Processing file: %s", originalFilePath)

	if !fw.shouldOptimizeFile(originalFilePath) {
		if fw.uploadToImmich(originalFilePath) {
			size := fileSize(originalFilePath)
			fw.recordProcessed(filepath.Base(originalFilePath), originalFilePath, size, size)
			fw.cleanupOriginalFile(originalFilePath)
		}
		return
	}

	tp, err := fw.createTaskProcessor(originalFilePath)
	if err != nil {
		fw.logger.Printf("Error creating task processor for %s: %v", originalFilePath, err)
		fw.recordFailure(filepath.Base(originalFilePath), originalFilePath, err)
		return
	}
	defer tp.Close()

	if err := tp.Process(fw.config.Tasks); err != nil {
		fw.handleProcessingError(originalFilePath, err)
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

// shouldOptimizeFile determines if a file should be processed for optimization
func (fw *FileWatcher) shouldOptimizeFile(filePath string) bool {
	extension := filepath.Ext(filePath)
	if !shouldProcessExtension(extension, fw.config.Tasks) {
		fw.logger.Printf("Skipping file %s (extension %s not configured for processing)", filePath, extension)
		return false
	}
	return true
}

// createTaskProcessor creates and configures a new task processor for the file
func (fw *FileWatcher) createTaskProcessor(filePath string) (*TaskProcessor, error) {
	tp, err := NewTaskProcessor(filePath)
	if err != nil {
		return nil, err
	}

	jobLogger := newCustomLogger(fw.logger, fmt.Sprintf("file %s: ", filePath))
	tp.SetLogger(jobLogger)

	if fw.profile != nil {
		tp.SetConfigDir(filepath.Dir(fw.profile.ConfigFile))
	}

	return tp, nil
}

// handleProcessingError handles errors that occur during file processing
func (fw *FileWatcher) handleProcessingError(filePath string, err error) {
	fw.logger.Printf("Error processing file %s: %v", filePath, err)
	fw.recordFailure(filepath.Base(filePath), filePath, err)
	if copyErr := copyFileToUndone(filePath, fw.watchDir, fw.profile.UndoneDir); copyErr != nil {
		fw.logger.Printf("Error copying file %s to undone directory: %v", filePath, copyErr)
	}
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
	return tp.ProcessedFile != nil && tp.ProcessedSize > 0 && tp.OriginalSize > tp.ProcessedSize
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

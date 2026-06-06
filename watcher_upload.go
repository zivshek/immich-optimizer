package main

import "path/filepath"

// uploadToImmich uploads a file to the Immich server, preserving the file's current name
func (fw *FileWatcher) uploadToImmich(uploadFilePath string) bool {
	return fw.uploadToImmichWithFilename(uploadFilePath, filepath.Base(uploadFilePath))
}

// uploadToImmichWithFilename uploads a file using the provided filename metadata
func (fw *FileWatcher) uploadToImmichWithFilename(uploadFilePath, filename string) bool {
	if err := fw.immichClient.UploadAssetWithFilename(uploadFilePath, filename); err != nil {
		fw.handleUploadError(uploadFilePath, err)
		return false
	}
	return true
}

// handleUploadError handles errors that occur during file upload
func (fw *FileWatcher) handleUploadError(filePath string, err error) {
	fw.logger.Printf("Error uploading file %s to Immich: %v", filePath, err)
	if copyErr := copyFileToUndone(filePath, fw.watchDir, fw.profile.UndoneDir); copyErr != nil {
		fw.logger.Printf("Error copying file %s to undone directory: %v", filePath, copyErr)
	}
}

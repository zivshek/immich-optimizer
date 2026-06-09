package main

import "path/filepath"

// uploadToImmich uploads a file to the Immich server, preserving the file's current name
func (fw *FileWatcher) uploadToImmich(uploadFilePath string) bool {
	return fw.uploadToImmichWithFilename(uploadFilePath, filepath.Base(uploadFilePath))
}

// uploadToImmichWithFilename uploads a file using the provided filename metadata
func (fw *FileWatcher) uploadToImmichWithFilename(uploadFilePath, filename string) bool {
	return fw.uploadToImmichWithFilenameFromSource(uploadFilePath, filename, uploadFilePath)
}

func (fw *FileWatcher) uploadToImmichWithFilenameFromSource(uploadFilePath, filename, sourceFilePath string) bool {
	if err := fw.immichClient.UploadAssetWithFilename(uploadFilePath, filename); err != nil {
		fw.handleUploadError(uploadFilePath, sourceFilePath, filename, err)
		return false
	}
	return true
}

// handleUploadError handles errors that occur during file upload
func (fw *FileWatcher) handleUploadError(uploadFilePath, sourceFilePath, filename string, err error) {
	fw.logger.Printf("Error uploading file %s to Immich: %v", uploadFilePath, err)
	fw.recordFailure(filename, sourceFilePath, err)
	if copyErr := copyFileToUndone(uploadFilePath, fw.watchDir, fw.profile.UndoneDir); copyErr != nil {
		fw.logger.Printf("Error copying file %s to undone directory: %v", uploadFilePath, copyErr)
	}
}

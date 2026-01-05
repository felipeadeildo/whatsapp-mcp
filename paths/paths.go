package paths

import (
	"os"
	"path/filepath"
)

// DataDir is the base data directory for the application.
const DataDir = "./data"

// Data subdirectories for organizing different types of data.
const (
	DataDBDir    = DataDir + "/db"
	DataMediaDir = DataDir + "/media"
)

// Storage paths for migrations and other persistent data.
const (
	MigrationsDir = "storage/migrations"
)

// File paths for databases, logs, and other files.
const (
	MessagesDBPath     = DataDBDir + "/messages.db"
	WhatsAppAuthDBPath = DataDBDir + "/whatsapp_auth.db"
	WhatsAppLogPath    = DataDir + "/whatsapp.log"
	QRCodePath         = "./qr.png"
)

// EnsureDataDirectories ensures that all required data directories exist.
func EnsureDataDirectories() error {
	dirs := []string{
		DataDir,
		DataDBDir,
		DataMediaDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

// GetMediaPath returns the full path for a media file given its relative path.
func GetMediaPath(relativePath string) string {
	return filepath.Join(DataMediaDir, relativePath)
}

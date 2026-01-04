package paths

import (
	"os"
	"path/filepath"
)

// base data directory
const DataDir = "./data"

// data subdirectories
const (
	DataDBDir    = DataDir + "/db"
	DataMediaDir = DataDir + "/media"
)

// storage paths
const (
	MigrationsDir = "storage/migrations"
)

// file paths
const (
	// database files
	MessagesDBPath     = DataDBDir + "/messages.db"
	WhatsAppAuthDBPath = DataDBDir + "/whatsapp_auth.db"

	// log files
	WhatsAppLogPath = DataDir + "/whatsapp.log"

	// other files
	QRCodePath = "./qr.png"
)

// EnsureDataDirectories ensures that all required data directories exist
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

// GetMediaPath returns the full path for a media file
func GetMediaPath(relativePath string) string {
	return filepath.Join(DataMediaDir, relativePath)
}

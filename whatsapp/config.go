package whatsapp

import (
	"strings"
	"whatsapp-mcp/config"
	"whatsapp-mcp/paths"
)

// MediaConfig holds configuration for media download behavior.
type MediaConfig struct {
	AutoDownloadEnabled     bool
	AutoDownloadFromHistory bool
	AutoDownloadMaxSize     int64 // bytes
	AutoDownloadTypes       map[string]bool
	StoragePath             string
}

// LoadMediaConfig loads media configuration from environment variables.
func LoadMediaConfig() MediaConfig {
	cfg := MediaConfig{
		AutoDownloadEnabled:     config.GetEnvBool("MEDIA_AUTO_DOWNLOAD_ENABLED", true),
		AutoDownloadFromHistory: config.GetEnvBool("MEDIA_AUTO_DOWNLOAD_FROM_HISTORY", false),
		AutoDownloadMaxSize:     config.GetEnvInt64("MEDIA_AUTO_DOWNLOAD_MAX_SIZE_MB", 10) * 1024 * 1024,
		StoragePath:             paths.DataMediaDir,
	}

	// parse allowed types
	typesStr := config.GetEnv("MEDIA_AUTO_DOWNLOAD_TYPES", "image,audio,sticker")
	cfg.AutoDownloadTypes = make(map[string]bool)
	for _, t := range strings.Split(typesStr, ",") {
		cfg.AutoDownloadTypes[strings.TrimSpace(t)] = true
	}

	return cfg
}

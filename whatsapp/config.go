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
		AutoDownloadFromHistory: config.GetEnvBool("MEDIA_AUTO_DOWNLOAD_FROM_HISTORY", true),
		AutoDownloadMaxSize:     config.GetEnvInt64("MEDIA_AUTO_DOWNLOAD_MAX_SIZE_MB", 20) * 1024 * 1024,
		StoragePath:             paths.DataMediaDir,
	}

	// parse allowed types — voice-notes (ptt) and documents/videos covered by default
	// so Subagents that need any incoming attachment have it on disk without manual config.
	typesStr := config.GetEnv("MEDIA_AUTO_DOWNLOAD_TYPES", "image,audio,ptt,sticker,document,video")
	cfg.AutoDownloadTypes = make(map[string]bool)
	for _, t := range strings.Split(typesStr, ",") {
		cfg.AutoDownloadTypes[strings.TrimSpace(t)] = true
	}

	return cfg
}

package whatsapp

import (
	"os"
	"strconv"
	"strings"
)

// media download configuration
type MediaConfig struct {
	AutoDownloadEnabled         bool
	AutoDownloadFromHistory     bool
	AutoDownloadMaxSize         int64 // bytes
	AutoDownloadTypes           map[string]bool
	StoragePath                 string
}

// media configuration from environment variables
func LoadMediaConfig() MediaConfig {
	config := MediaConfig{
		AutoDownloadEnabled:     getEnvBool("MEDIA_AUTO_DOWNLOAD_ENABLED", true),
		AutoDownloadFromHistory: getEnvBool("MEDIA_AUTO_DOWNLOAD_FROM_HISTORY", false),
		AutoDownloadMaxSize:     getEnvInt64("MEDIA_AUTO_DOWNLOAD_MAX_SIZE_MB", 10) * 1024 * 1024,
		StoragePath:             getEnv("MEDIA_STORAGE_PATH", "./data/media"),
	}

	// parse allowed types
	typesStr := getEnv("MEDIA_AUTO_DOWNLOAD_TYPES", "image,audio,sticker")
	config.AutoDownloadTypes = make(map[string]bool)
	for _, t := range strings.Split(typesStr, ",") {
		config.AutoDownloadTypes[strings.TrimSpace(t)] = true
	}

	return config
}

// gets environment variable with fallback default
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// gets boolean environment variable with fallback default
func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return b
}

// gets int64 environment variable with fallback default
func getEnvInt64(key string, defaultVal int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return i
}
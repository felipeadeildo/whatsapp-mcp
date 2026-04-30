package whatsapp

import (
	"fmt"
	"os"
	"path/filepath"

	"whatsapp-mcp/paths"
)

// dumpTranscript writes text to data/transcripts/<messageID>.txt, creating
// the directory on demand. The path is predictable so callers can locate
// transcripts without inspecting the return value -- it's returned only for
// log/error messages. Any error here is non-fatal at the call site: the
// transcript itself is already in the cache and in the tool's response, so
// the file dump is purely an audit/UI-workaround convenience.
func dumpTranscript(messageID, text string) (string, error) {
	dir := filepath.Join(paths.DataDir, "transcripts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create transcripts dir: %w", err)
	}
	path := filepath.Join(dir, messageID+".txt")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return "", fmt.Errorf("write transcript: %w", err)
	}
	return path, nil
}

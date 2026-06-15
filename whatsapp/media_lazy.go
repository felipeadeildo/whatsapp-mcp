package whatsapp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"whatsapp-mcp/paths"
	"whatsapp-mcp/storage"
)

// cdnBase is the WhatsApp media CDN host. The encrypted media URL is
// cdnBase + media_metadata.direct_path (which already carries auth params
// like ?ccb=...&oh=...&oe=...).
const cdnBase = "https://mmg.whatsapp.net"

// hkdfInfoForMime returns the HKDF "info" string WhatsApp uses to derive
// per-mime keys from a media_key. Mirrors the logic in download_audio.py
// and the WhatsApp Web protocol.
func hkdfInfoForMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "WhatsApp Image Keys"
	case strings.HasPrefix(mime, "video/"):
		return "WhatsApp Video Keys"
	case strings.HasPrefix(mime, "audio/"):
		return "WhatsApp Audio Keys"
	default:
		return "WhatsApp Document Keys"
	}
}

// hkdfExpand performs HKDF-Extract+Expand (RFC 5869) with a 32-byte zero salt,
// matching the Python implementation in download_audio.py.
func hkdfExpand(key, info []byte, length int) []byte {
	salt := make([]byte, 32)
	mac := hmac.New(sha256.New, salt)
	mac.Write(key)
	prk := mac.Sum(nil)

	var okm []byte
	var t []byte
	counter := byte(1)
	for len(okm) < length {
		mac := hmac.New(sha256.New, prk)
		mac.Write(t)
		mac.Write(info)
		mac.Write([]byte{counter})
		t = mac.Sum(nil)
		okm = append(okm, t...)
		counter++
	}
	return okm[:length]
}

// pkcs7Unpad strips PKCS7 padding from a block-aligned plaintext.
func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, errors.New("empty plaintext")
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > aes.BlockSize {
		return nil, fmt.Errorf("invalid padding length: %d", pad)
	}
	if pad > len(b) {
		return nil, errors.New("padding longer than data")
	}
	for i := len(b) - pad; i < len(b); i++ {
		if int(b[i]) != pad {
			return nil, errors.New("malformed PKCS7 padding")
		}
	}
	return b[:len(b)-pad], nil
}

// fetchCDN downloads the encrypted blob for a direct_path. WhatsApp's CDN
// returns 404 once the URL's `oe=` expiry passes.
func fetchCDN(ctx context.Context, directPath string) ([]byte, error) {
	url := cdnBase + directPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// whatsmeow uses a User-Agent like this; mimicking keeps us on the safe path.
	req.Header.Set("User-Agent", "WhatsApp/2.24.6.78 A")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CDN fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("CDN returned %d (media expired or revoked)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CDN returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// decryptMedia decrypts a WhatsApp media payload using the standard
// HKDF(media_key) → IV/cipher_key/mac_key derivation, validates the trailing
// 10-byte HMAC truncation, and PKCS7-unpads the result.
func decryptMedia(mediaKey []byte, info string, encData []byte) ([]byte, error) {
	if len(mediaKey) == 0 {
		return nil, errors.New("missing media_key")
	}
	if len(encData) < aes.BlockSize+10 {
		return nil, errors.New("encrypted payload too short")
	}

	derived := hkdfExpand(mediaKey, []byte(info), 112)
	iv := derived[:16]
	cipherKey := derived[16:48]
	macKey := derived[48:80]

	ciphertext := encData[:len(encData)-10]
	receivedMAC := encData[len(encData)-10:]

	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ciphertext)
	expectedMAC := mac.Sum(nil)[:10]
	if !hmac.Equal(receivedMAC, expectedMAC) {
		return nil, errors.New("MAC verification failed")
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d not block-aligned", len(ciphertext))
	}
	block, err := aes.NewCipher(cipherKey)
	if err != nil {
		return nil, fmt.Errorf("AES init: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	return pkcs7Unpad(plaintext)
}

// subdirForMime mirrors generateMediaFilePath — keep the on-disk layout
// consistent between auto-download and lazy-download paths.
func subdirForMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "images"
	case strings.HasPrefix(mime, "video/"):
		return "videos"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	default:
		return "documents"
	}
}

// persistDecrypted writes the plaintext to data/media/<subdir>/<filename>
// using the same naming convention as the auto-download path. Returns the
// relative path stored in media_metadata.file_path.
func (c *Client) persistDecrypted(meta *storage.MediaMetadata, plaintext []byte) (string, error) {
	subdir := subdirForMime(meta.MimeType)
	timestamp := time.Now().Format("20060102_150405")
	safeName := sanitizeFilename(meta.FileName)
	if safeName == "" {
		ext := mimeToExtension(meta.MimeType)
		safeName = fmt.Sprintf("media_%s%s", timestamp, ext)
	}
	idPrefix := meta.MessageID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	fileName := fmt.Sprintf("%s_%s_%s", idPrefix, timestamp, safeName)
	absPath := filepath.Join(c.mediaConfig.StoragePath, subdir, fileName)

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(absPath, plaintext, 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	relPath, err := filepath.Rel(c.mediaConfig.StoragePath, absPath)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}
	return relPath, nil
}

// EnsureMediaDownloadResult describes the outcome of EnsureMediaDownloaded.
type EnsureMediaDownloadResult struct {
	FilePath          string // relative path under data/media/
	AbsolutePath      string // absolute path on disk
	WasAlreadyOnDisk  bool   // true if no fetch was needed
	BytesWritten      int    // size of decrypted plaintext (0 if was_already_on_disk)
	ExistingStatus    string // download_status before this call
	ResolvedMimeType  string // helps callers pick subsequent processing
}

// EnsureMediaDownloaded guarantees the media for messageID is decrypted and
// written to data/media/. If the file is already on disk and the metadata
// says download_status='downloaded', it is a no-op. Otherwise the routine
// re-fetches via the CDN using direct_path + media_key from media_metadata,
// decrypts, writes to disk, and updates download_status to 'downloaded'.
//
// force=true bypasses the on-disk check and always re-downloads (use when
// you suspect the file is corrupt). On CDN expiration (404/410), the
// download_status is moved to 'expired' so callers don't keep retrying.
func (c *Client) EnsureMediaDownloaded(ctx context.Context, messageID string, force bool) (*EnsureMediaDownloadResult, error) {
	if c.mediaStore == nil {
		return nil, errors.New("client not wired with media store")
	}

	meta, err := c.mediaStore.GetMediaMetadata(messageID)
	if err != nil {
		return nil, fmt.Errorf("lookup media: %w", err)
	}
	if meta == nil {
		return nil, fmt.Errorf("no media metadata for message %s", messageID)
	}

	result := &EnsureMediaDownloadResult{
		ExistingStatus:   meta.DownloadStatus,
		ResolvedMimeType: meta.MimeType,
	}

	// fast path: already on disk
	if !force && meta.DownloadStatus == "downloaded" && meta.FilePath != "" {
		abs := paths.GetMediaPath(meta.FilePath)
		if stat, statErr := os.Stat(abs); statErr == nil && stat.Size() > 0 {
			result.FilePath = meta.FilePath
			result.AbsolutePath = abs
			result.WasAlreadyOnDisk = true
			return result, nil
		}
		// file gone or empty — fall through to re-download
		c.log.Warnf("Media %s marked downloaded but file missing/empty at %s; re-fetching", messageID, abs)
	}

	if len(meta.MediaKey) == 0 || meta.DirectPath == "" {
		return nil, fmt.Errorf("message %s lacks media_key or direct_path; cannot lazy-download", messageID)
	}

	encData, err := fetchCDN(ctx, meta.DirectPath)
	if err != nil {
		// expiration is a permanent failure — record it so the next caller
		// fails fast instead of hammering the CDN.
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "410") || strings.Contains(errStr, "expired") {
			_ = c.mediaStore.UpdateDownloadStatus(messageID, "expired", nil, err)
		} else {
			_ = c.mediaStore.UpdateDownloadStatus(messageID, "failed", nil, err)
		}
		return nil, fmt.Errorf("CDN fetch for %s: %w", messageID, err)
	}

	plaintext, err := decryptMedia(meta.MediaKey, hkdfInfoForMime(meta.MimeType), encData)
	if err != nil {
		_ = c.mediaStore.UpdateDownloadStatus(messageID, "failed", nil, err)
		return nil, fmt.Errorf("decrypt %s: %w", messageID, err)
	}

	// optional integrity check: if metadata carries file_sha256, verify
	if len(meta.FileSHA256) == 32 {
		actual := sha256.Sum256(plaintext)
		if !bytes.Equal(actual[:], meta.FileSHA256) {
			c.log.Warnf("SHA256 mismatch for %s (continuing, payload still decrypted OK)", messageID)
		}
	}

	relPath, err := c.persistDecrypted(meta, plaintext)
	if err != nil {
		return nil, fmt.Errorf("persist %s: %w", messageID, err)
	}
	if err := c.mediaStore.UpdateDownloadStatus(messageID, "downloaded", &relPath, nil); err != nil {
		c.log.Errorf("Failed to update download_status for %s: %v", messageID, err)
	}

	result.FilePath = relPath
	result.AbsolutePath = paths.GetMediaPath(relPath)
	result.BytesWritten = len(plaintext)
	c.log.Infof("Lazy-downloaded media %s (%d bytes, mime=%s)", messageID, result.BytesWritten, meta.MimeType)
	return result, nil
}

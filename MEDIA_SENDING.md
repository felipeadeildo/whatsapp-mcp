# Media Sending Feature

## Overview

This feature adds the ability to send media files (images, videos, documents, and audio) through WhatsApp using the MCP server. It supports both local file paths (for desktop/local usage) and base64-encoded data (for web applications/remote usage).

## Features

### Supported Media Types

| Media Type | Formats | Max Size | MCP Tool |
|-----------|---------|----------|----------|
| **Images** | JPEG, PNG, WebP | 5 MB | `send_image` |
| **Videos** | MP4, 3GP | 100 MB | `send_video` |
| **Documents** | PDF, DOCX, ZIP, etc. | 2 GB | `send_document` |
| **Audio** | MP3, OGG, AAC, M4A | 16 MB | `send_audio` |

### Key Features

- **Hybrid File Loading**: Support for both local file paths AND base64-encoded data
- **File Size Validation**: Automatic enforcement of official WhatsApp file size limits
- **MIME Type Detection**: Automatic detection from file extension or content
- **Database Persistence**: All sent media messages are saved to the database
- **Error Handling**: Comprehensive validation with clear error messages
- **Flexible Input**: XOR validation ensures exactly one input method is used

## MCP Tools Reference

### 1. send_image

Send an image to a WhatsApp chat.

**Parameters:**
- `chat_jid` (required): Target chat JID from `find_chat` or `list_chats`
- `file_path` (optional): Local path to image file (e.g., `C:\images\photo.jpg`)
- `file_data` (optional): Base64-encoded image data
- `caption` (optional): Caption text for the image

**Validation Rules:**
- Exactly one of `file_path` OR `file_data` must be provided (not both, not neither)
- File size must be ≤ 5 MB
- Supported formats: JPEG, PNG, WebP

**Example:**
```json
{
  "name": "send_image",
  "arguments": {
    "chat_jid": "5511999999999@s.whatsapp.net",
    "file_path": "C:\\images\\vacation.jpg",
    "caption": "Check out this view!"
  }
}
```

### 2. send_video

Send a video to a WhatsApp chat.

**Parameters:**
- `chat_jid` (required): Target chat JID
- `file_path` (optional): Local path to video file
- `file_data` (optional): Base64-encoded video data
- `caption` (optional): Caption text for the video

**Validation Rules:**
- Exactly one of `file_path` OR `file_data` must be provided
- File size must be ≤ 100 MB
- Supported formats: MP4, 3GP

**Example:**
```json
{
  "name": "send_video",
  "arguments": {
    "chat_jid": "5511999999999@s.whatsapp.net",
    "file_path": "C:\\videos\\clip.mp4",
    "caption": "Watch this!"
  }
}
```

### 3. send_document

Send a document to a WhatsApp chat.

**Parameters:**
- `chat_jid` (required): Target chat JID
- `file_path` (optional): Local path to document file
- `file_data` (optional): Base64-encoded document data
- `filename` (conditional): Filename for the document (required if using `file_data`)

**Validation Rules:**
- Exactly one of `file_path` OR `file_data` must be provided
- If using `file_data`, `filename` must be provided
- File size must be ≤ 2 GB
- Supported formats: PDF, DOCX, XLSX, PPTX, ZIP, etc.

**Example:**
```json
{
  "name": "send_document",
  "arguments": {
    "chat_jid": "5511999999999@s.whatsapp.net",
    "file_path": "C:\\docs\\report.pdf"
  }
}
```

**Example with base64:**
```json
{
  "name": "send_document",
  "arguments": {
    "chat_jid": "5511999999999@s.whatsapp.net",
    "file_data": "JVBERi0xLjQKJ...",
    "filename": "report.pdf"
  }
}
```

### 4. send_audio

Send an audio file to a WhatsApp chat.

**Parameters:**
- `chat_jid` (required): Target chat JID
- `file_path` (optional): Local path to audio file
- `file_data` (optional): Base64-encoded audio data

**Validation Rules:**
- Exactly one of `file_path` OR `file_data` must be provided
- File size must be ≤ 16 MB
- Supported formats: MP3, OGG, AAC, M4A

**Example:**
```json
{
  "name": "send_audio",
  "arguments": {
    "chat_jid": "5511999999999@s.whatsapp.net",
    "file_path": "C:\\audio\\message.mp3"
  }
}
```

## Usage Workflow

### Step 1: Find the Chat JID

Before sending media, you need the target chat's JID:

```json
{
  "name": "list_chats",
  "arguments": {
    "limit": 50
  }
}
```

Or search for a specific chat:

```json
{
  "name": "find_chat",
  "arguments": {
    "search": "John*"
  }
}
```

The response will include the `chat_jid` (e.g., `5511999999999@s.whatsapp.net`).

### Step 2: Send Media

Use any of the media sending tools with the `chat_jid` from Step 1.

### Step 3: Verify Success

The tool will return a success message or error:
- Success: `"Image sent successfully to 5511999999999@s.whatsapp.net"`
- Error: Specific error message describing what went wrong

## Implementation Details

### File Size Limits

Official WhatsApp file size limits (2025):

```go
const (
    maxImageSize    = 5 * 1024 * 1024      // 5 MB
    maxVideoSize    = 100 * 1024 * 1024    // 100 MB
    maxAudioSize    = 16 * 1024 * 1024     // 16 MB
    maxDocumentSize = 2 * 1024 * 1024 * 1024 // 2 GB
)
```

### Client Methods

#### whatsapp/client.go

**SendImage**
```go
func (c *Client) SendImage(ctx context.Context, chatJID, filePath, fileData, caption string) error
```
Sends an image to a WhatsApp chat. Supports both local file paths and base64-encoded data.

**SendVideo**
```go
func (c *Client) SendVideo(ctx context.Context, chatJID, filePath, fileData, caption string) error
```
Sends a video to a WhatsApp chat.

**SendDocument**
```go
func (c *Client) SendDocument(ctx context.Context, chatJID, filePath, fileData, filename string) error
```
Sends a document to a WhatsApp chat.

**SendAudio**
```go
func (c *Client) SendAudio(ctx context.Context, chatJID, filePath, fileData string) error
```
Sends an audio file to a WhatsApp chat.

### Helper Functions

#### loadFileData
```go
func (c *Client) loadFileData(filePath, fileData string) ([]byte, string, error)
```
Loads file data from either a local path or base64-encoded data. Returns the file bytes, detected filename, and any error.

**Logic:**
- If `filePath` is provided: reads from disk, extracts filename from path
- If `fileData` is provided: decodes from base64
- Validates that exactly one input is provided (XOR validation)

#### detectMimeType
```go
func (c *Client) detectMimeType(filename string, data []byte) string
```
Determines the MIME type of a file from its filename and content.

**Logic:**
1. First attempts to detect from file extension
2. Falls back to `http.DetectContentType` for base64 data
3. Returns `"application/octet-stream"` if unable to detect

#### validateFileSize
```go
func (c *Client) validateFileSize(data []byte, maxSize int, mediaType string) error
```
Checks if a file's size exceeds the maximum allowed for its media type.

**Returns error if:**
- File size exceeds maxSize
- Error message includes the actual size, limit, and media type

### MCP Tool Registration

#### mcp/tools.go

All four tools are registered with comprehensive parameter descriptions:
- Required/optional parameters are clearly marked
- Size limits are documented in tool descriptions
- Usage instructions are included (file_path vs file_data)

### Handler Implementation

#### mcp/handlers.go

**Common Handler Pattern:**

1. Extract and validate `chat_jid` (required parameter)
2. Extract `file_path` and `file_data` (optional parameters)
3. **XOR Validation**: Ensure exactly one is provided
   - Error if both are empty: `"either file_path or file_data must be provided"`
   - Error if both are provided: `"only one of file_path or file_data should be provided, not both"`
4. Check WhatsApp login status
5. Call appropriate client method
6. Return success or error response

**Example Handler (send_image):**
```go
func (m *MCPServer) handleSendImage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // Extract chat_jid
    chatJID, err := request.RequireString("chat_jid")
    if err != nil {
        return mcp.NewToolResultError("chat_jid parameter is required"), nil
    }

    // Extract file_path and file_data
    filePath := request.GetString("file_path", "")
    fileData := request.GetString("file_data", "")
    caption := request.GetString("caption", "")

    // Validate exactly one is provided
    if filePath == "" && fileData == "" {
        return mcp.NewToolResultError("either file_path or file_data must be provided"), nil
    }
    if filePath != "" && fileData != "" {
        return mcp.NewToolResultError("only one of file_path or file_data should be provided, not both"), nil
    }

    // Check login status
    if !m.wa.IsLoggedIn() {
        return mcp.NewToolResultError("WhatsApp is not connected"), nil
    }

    // Send image
    err = m.wa.SendImage(ctx, chatJID, filePath, fileData, caption)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("failed to send image: %v", err)), nil
    }

    return mcp.NewToolResultText(fmt.Sprintf("Image sent successfully to %s", chatJID)), nil
}
```

## Database Persistence

All sent media messages are automatically saved to the database with the following structure:

```go
storage.Message{
    ID:          resp.ID,           // WhatsApp message ID
    ChatJID:     chatJID,           // Target chat JID
    SenderJID:   resp.Sender.String(), // Sender JID (your number)
    Text:        caption,           // Caption text
    Timestamp:   resp.Timestamp,    // Send timestamp
    IsFromMe:    true,              // Marked as sent by user
    MessageType: "image/video/document/audio", // Media type
}
```

## Error Handling

### Common Errors

**"chat_jid parameter is required"**
- The `chat_jid` parameter was not provided
- Solution: Include `chat_jid` in your request

**"either file_path or file_data must be provided"**
- Neither `file_path` nor `file_data` was provided
- Solution: Provide exactly one of them

**"only one of file_path or file_data should be provided, not both"**
- Both `file_path` and `file_data` were provided
- Solution: Provide only one of them

**"failed to read file: ..."**
- Local file could not be read
- Solution: Check file path, ensure file exists and is readable

**"failed to decode base64 data: ..."**
- Base64 data is invalid
- Solution: Ensure data is properly base64-encoded

**"file size exceeds limit of X MB for media type"**
- File is too large for WhatsApp
- Solution: Compress or reduce file size before sending

**"WhatsApp is not connected"**
- WhatsApp client is not logged in
- Solution: Scan QR code to authenticate

**"invalid chat JID: ..."**
- The JID format is incorrect
- Solution: Use JID from `list_chats` or `find_chat`

## Testing

### Using MCP Inspector

Start the inspector:
```bash
npx @modelcontextprotocol/inspector http://localhost:8080/mcp/change-me-in-production
```

This provides a web UI to test all tools interactively.

### Using curl

**Test send_image:**
```bash
curl -X POST http://localhost:8080/mcp/change-me-in-production \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "send_image",
      "arguments": {
        "chat_jid": "5511999999999@s.whatsapp.net",
        "file_path": "C:\\path\\to\\image.jpg",
        "caption": "Test image"
      }
    }
  }'
```

### Test Files

Recommended test files:
- **Image**: Small JPEG/PNG file (< 5 MB)
- **Video**: Short MP4 clip (< 100 MB)
- **Document**: PDF file (< 2 GB)
- **Audio**: MP3 file (< 16 MB)

## Architecture Decisions

### Hybrid Approach

**Decision:** Support both `file_path` and `file_data` parameters

**Rationale:**
- **Local usage (Claude Code)**: File paths are more efficient and don't require base64 encoding
- **Remote usage (Web Apps)**: Base64 data allows sending files from clients without file system access
- **Flexibility**: Users can choose the method that best fits their use case

**Implementation:**
- XOR validation ensures exactly one method is used
- Single client method handles both cases internally
- Transparent to the end user

### File Size Limits

**Decision:** Enforce official WhatsApp limits at the handler level

**Rationale:**
- Prevents failed uploads and wasted bandwidth
- Provides clear error messages before attempting upload
- Aligns with WhatsApp's actual capabilities

**Implementation:**
- Constants defined for each media type
- Validation occurs before upload attempt
- Error messages include actual size, limit, and media type

### MIME Type Detection

**Decision:** Auto-detect MIME type from file extension or content

**Rationale:**
- Reduces user burden (no need to specify MIME type manually)
- Improves reliability (automatic is less error-prone than manual)
- Supports both file paths (extension-based) and base64 (content-based)

**Implementation:**
- Extension-based detection for file paths (faster, more accurate)
- Content-based detection fallback for base64 data
- Default to `"application/octet-stream"` if detection fails

## Future Enhancements

Potential improvements for future versions:

1. **Progress Callbacks**: Report upload progress for large files
2. **Thumbnail Generation**: Generate thumbnails for images/videos
3. **Batch Sending**: Send multiple files in one request
4. **Compression**: Automatic compression for oversized files
5. **File Format Conversion**: Convert unsupported formats to supported ones
6. **Metadata Extraction**: Extract and preserve EXIF/metadata from images
7. **Retry Logic**: Automatic retry for failed uploads
8. **Streaming Support**: Stream large files instead of loading entirely into memory

## References

- [Official WhatsApp FAQ - Sending Media](https://faq.whatsapp.com/web/chats/how-to-send-media/)
- [WhatsApp File Size Limits](https://faq.whatsapp.com/759301289012856)
- [whatsmeow Documentation](https://github.com/tulir/whatsmeow)
- [MCP Specification](https://modelcontextprotocol.io/)

## Contributing

When contributing to this feature:

1. **Follow Go Conventions**: Use `gofmt` and `golint`
2. **Add Tests**: Write tests for new functionality
3. **Update Documentation**: Keep this file in sync with code changes
4. **Preserve Backwards Compatibility**: Don't break existing integrations
5. **Error Handling**: Always return descriptive errors
6. **Size Limits**: Never exceed official WhatsApp limits
7. **Database Persistence**: Always save sent messages to database

See `CONTRIBUTING.md` for detailed guidelines.

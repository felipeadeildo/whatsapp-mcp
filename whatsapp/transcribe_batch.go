// whatsapp/transcribe_batch.go
package whatsapp

import (
	"context"
	"sync"
)

// BatchTranscriptItem is the per-message outcome of a batch transcription.
type BatchTranscriptItem struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
}

// CollectAudioMessageIDs returns the IDs of audio/voice-note (ptt) messages in a
// chat, up to limit, in the order GetChatMessages returns them. Used by the batch
// tool when called with a chat_jid instead of an explicit message_ids list.
func (c *Client) CollectAudioMessageIDs(chatJID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	// Over-fetch because we filter to audio/ptt; cap the scan to keep it bounded.
	msgs, err := c.store.GetChatMessages(chatJID, limit*5, 0)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, limit)
	for _, m := range msgs {
		if m.MessageType == "audio" || m.MessageType == "ptt" {
			ids = append(ids, m.ID)
			if len(ids) >= limit {
				break
			}
		}
	}
	return ids, nil
}

// TranscribeBatch transcribes many messages concurrently with a bounded worker
// pool (TranscribeConfig.BatchConcurrency). It reuses TranscribeMessage per item,
// so each gets the transcript cache, lazy CDN download, and cache-save for free.
// Results are PARTIAL: a failing item records its error and does not abort the rest.
// Output order matches the input messageIDs order.
func (c *Client) TranscribeBatch(ctx context.Context, messageIDs []string) []BatchTranscriptItem {
	conc := c.transcribeConfig.BatchConcurrency
	if conc < 1 {
		conc = 1
	}
	results := make([]BatchTranscriptItem, len(messageIDs))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup

	for i, id := range messageIDs {
		// Respect cancellation: stop launching new work if ctx is done.
		select {
		case <-ctx.Done():
			results[i] = BatchTranscriptItem{MessageID: id, Error: ctx.Err().Error()}
			continue
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, id string) {
			defer wg.Done()
			defer func() { <-sem }()
			item := BatchTranscriptItem{MessageID: id}
			text, err := c.TranscribeMessage(ctx, id)
			if err != nil {
				item.Error = err.Error()
			} else {
				item.Text = text
			}
			results[i] = item
		}(i, id)
	}
	wg.Wait()
	return results
}

// internal/domain/event.go
package domain

// WhatsAppEvent represents the full data from a WhatsApp message
type WhatsAppEvent struct {
	ID                string `json:"id"`
	WhatsAppMessageID string `json:"whatsapp_message_id"`
	FromNumber        string `json:"from_number"`
	Command           string `json:"command"`
	Payload           string `json:"payload"`
	OriginalText      string `json:"original_text"`
	Timestamp         int64  `json:"timestamp"`
}

// WhatsAppWebhookPayload matches the WhatsApp Business API webhook format
type WhatsAppWebhookPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Value struct {
				Messages []struct {
					From string `json:"from"`
					ID   string `json:"id"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
					Timestamp string `json:"timestamp"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

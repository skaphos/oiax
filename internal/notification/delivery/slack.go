package delivery

import "github.com/skaphos/oiax/internal/notification"

func slack(p notification.DeliveryPayloadV1, facts string) any {
	var blocks []any
	for _, text := range []string{p.Message.Title, p.Message.Body, facts} {
		runes := []rune(text)
		for len(runes) > 0 {
			length := min(len(runes), 3000)
			blocks = append(blocks, map[string]any{"type": "section", "text": map[string]any{"type": "plain_text", "text": string(runes[:length]), "emoji": false}})
			runes = runes[length:]
		}
	}
	blocks = append(blocks, map[string]any{"type": "actions", "elements": []any{map[string]any{"type": "button", "text": map[string]string{"type": "plain_text", "text": "View request"}, "url": p.Event.Request.URL}}})
	return map[string]any{"text": "Oiax managed request notification", "blocks": blocks, "unfurl_links": false, "unfurl_media": false}
}

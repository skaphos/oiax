package delivery

import "github.com/skaphos/oiax/v2/internal/notification"

func teams(p notification.DeliveryPayloadV1, facts string) any {
	block := func(text string) any {
		return map[string]any{"type": "RichTextBlock", "inlines": []any{map[string]any{"type": "TextRun", "text": text}}}
	}
	return map[string]any{"type": "message", "attachments": []any{map[string]any{"contentType": "application/vnd.microsoft.card.adaptive", "contentUrl": nil, "content": map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "type": "AdaptiveCard", "version": "1.2",
		"body":    []any{block(p.Message.Title), block(p.Message.Body), block(facts)},
		"actions": []any{map[string]string{"type": "Action.OpenUrl", "title": "View request", "url": p.Event.Request.URL}},
	}}}}
}

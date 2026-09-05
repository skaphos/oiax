package delivery

import "testing"

func FuzzNotificationEndpoint(f *testing.F) {
	for _, s := range []string{"https://example.com/webhook", "http://127.0.0.1/", "https://user:password@example.com/", "https://[::1]/", "https://example.com/#fragment", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			t.Skip()
		}
		_, _ = validateEndpoint(raw)
	})
}

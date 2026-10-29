package twitch

import (
	"encoding/json"
	"testing"
)

func TestParseEventSubWelcomeAndNotification(t *testing.T) {
	fixtures := []struct {
		raw                   string
		typ, session, eventID string
	}{
		{`{"metadata":{"message_type":"session_welcome"},"payload":{"session":{"id":"session-1"}}}`, "session_welcome", "session-1", ""},
		{`{"metadata":{"message_type":"notification"},"payload":{"event":{"message_id":"message-1","message":{"text":"hello"}}}}`, "notification", "", "message-1"},
	}
	for _, fixture := range fixtures {
		got, err := ParseEventSub([]byte(fixture.raw))
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != fixture.typ || got.SessionID != fixture.session {
			t.Fatalf("unexpected envelope: %#v", got)
		}
		if fixture.eventID != "" {
			var event struct {
				MessageID string `json:"message_id"`
			}
			if err := json.Unmarshal(got.Event, &event); err != nil {
				t.Fatal(err)
			}
			if event.MessageID != fixture.eventID {
				t.Fatalf("unexpected event ID %q", event.MessageID)
			}
		}
	}
}

func TestParseEventSubRejectsInvalidEnvelope(t *testing.T) {
	for _, raw := range []string{`{`, `{}`, `{"metadata":{"message_type":""}}`} {
		if _, err := ParseEventSub([]byte(raw)); err == nil {
			t.Fatalf("expected rejection for %q", raw)
		}
	}
}

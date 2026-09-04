package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimeJSONRoundTrip(t *testing.T) {
	original := time.Date(2026, time.August, 28, 14, 30, 45, 0, time.FixedZone("UTC-6", -6*60*60))
	encoded, err := json.Marshal(AsJTime(original))
	if err != nil {
		t.Fatalf("Marshal() returned an error: %v", err)
	}
	if got, want := string(encoded), `"2026-08-28T14:30:45-06:00"`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}

	var decoded Time
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() returned an error: %v", err)
	}
	if !decoded.Time.Equal(original) {
		t.Fatalf("round trip = %s, want same instant as %s", decoded.Time, original)
	}
}

func TestTimeUnmarshalAcceptsRFC3339(t *testing.T) {
	var value Time
	if err := json.Unmarshal([]byte(`"2026-08-28T20:30:45Z"`), &value); err != nil {
		t.Fatalf("Unmarshal() returned an error: %v", err)
	}
	if got, want := value.Format(time.RFC3339), "2026-08-28T20:30:45Z"; got != want {
		t.Fatalf("decoded time = %q, want %q", got, want)
	}
}

func TestTimeUnmarshalRejectsInvalidValues(t *testing.T) {
	for _, encoded := range []string{
		`"not-a-time"`,
		`"2026-08-28"`,
		`null`,
	} {
		t.Run(encoded, func(t *testing.T) {
			var value Time
			if err := json.Unmarshal([]byte(encoded), &value); err == nil {
				t.Fatalf("Unmarshal(%s) succeeded", encoded)
			}
		})
	}
}

func TestAsJTimePreservesWrappedTime(t *testing.T) {
	original := time.Date(2026, time.August, 28, 20, 30, 45, 123, time.UTC)
	wrapped := AsJTime(original)
	if !wrapped.Time.Equal(original) {
		t.Fatalf("AsJTime() = %s, want %s", wrapped.Time, original)
	}
}

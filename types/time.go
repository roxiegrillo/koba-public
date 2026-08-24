package types

import (
	"encoding/json"
	"time"
)

// Time is a time.Time whose JSON form is an RFC 3339 string.
type Time struct {
	time.Time
}

// AsJTime returns t as a Time.
func AsJTime(t time.Time) Time {
	return Time{Time: t}
}

// MarshalJSON writes the instant as an RFC 3339 string.
func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format(time.RFC3339))
}

// UnmarshalJSON reads an RFC 3339 string. Any other JSON value, null
// included, is an error and leaves the zero instant.
func (t *Time) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		t.Time = time.Time{}
		return err
	}
	parsed, err := time.Parse(time.RFC3339, text)
	t.Time = parsed
	return err
}

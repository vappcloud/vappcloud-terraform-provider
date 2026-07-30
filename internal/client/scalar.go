package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Version is the protobuf JSON representation of an int64 concurrency or
// revision field. Protobuf JSON encodes 64-bit integers as quoted decimal
// strings, while accepting numeric values keeps the client compatible with
// pre-transcoder and test endpoints during rollout.
type Version int64

func (v Version) Int64() int64 {
	return int64(v)
}

func (v Version) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(v), 10))
}

func (v *Version) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*v = 0
		return nil
	}
	if raw[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return fmt.Errorf("decode quoted int64: %w", err)
		}
		parsed, err := strconv.ParseInt(encoded, 10, 64)
		if err != nil {
			return fmt.Errorf("decode quoted int64 %q: %w", encoded, err)
		}
		*v = Version(parsed)
		return nil
	}
	parsed, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return fmt.Errorf("decode int64 %q: %w", string(raw), err)
	}
	*v = Version(parsed)
	return nil
}

package audit

import (
	"encoding/json"
	"time"
)

func marshalWithTS(e interface{}, ts time.Time) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	// Replace ts field with millisecond-precision RFC3339Nano.
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["ts"] = ts.UTC().Format("2006-01-02T15:04:05.000Z")
	return json.Marshal(m)
}

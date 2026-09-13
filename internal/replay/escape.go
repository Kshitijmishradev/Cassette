package replay

import "encoding/json"

// jsonEscape quotes a string for embedding in a hand-built JSON message.
func jsonEscape(s string) ([]byte, error) {
	return json.Marshal(s)
}

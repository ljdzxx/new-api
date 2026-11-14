package common

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// deleteJSONPath edits only the selected member. Repeat for duplicate keys so
// a disabled option cannot survive under a second copy of the same key.
func deleteJSONPath(body []byte, path string) ([]byte, error) {
	for gjson.GetBytes(body, path).Exists() {
		next, err := sjson.DeleteBytes(body, path)
		if err != nil {
			return nil, err
		}
		if len(next) >= len(body) {
			return nil, fmt.Errorf("cannot remove JSON member %q", path)
		}
		body = next
	}
	return body, nil
}

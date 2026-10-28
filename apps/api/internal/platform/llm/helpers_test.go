package llm_test

import (
	"encoding/json"
	"net/http"
)

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

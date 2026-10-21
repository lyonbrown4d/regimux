package container

import (
	"encoding/json"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func manifestBodyMediaType(body []byte) string {
	if jsonbudget.Check(body, jsonbudget.DefaultLimits()) != nil {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return ""
	}
	var mediaType string
	if err := json.Unmarshal(fields["mediaType"], &mediaType); err != nil {
		return ""
	}
	return normalizeManifestMediaType(mediaType)
}

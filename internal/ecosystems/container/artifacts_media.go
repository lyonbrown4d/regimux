package container

import "encoding/json"

func manifestBodyMediaType(body []byte) string {
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

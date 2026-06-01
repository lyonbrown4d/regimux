package distribution

type ManifestUnknownDetail struct {
	Repository string `json:"repo"`
	Reference  string `json:"reference"`
}

type BlobUnknownDetail struct {
	Repository string `json:"repo"`
	Digest     string `json:"digest"`
}

type DigestMismatchDetail struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type UnsupportedDetail struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type UpstreamStatusDetail struct {
	Status int    `json:"status"`
	Kind   string `json:"kind,omitempty"`
}

func DigestMismatch(expected, actual string) *ErrorList {
	return ErrDigestMismatch.WithDetail(DigestMismatchDetail{
		Expected: expected,
		Actual:   actual,
	})
}

func Unsupported(method, path string) *ErrorList {
	return ErrUnsupported.WithDetail(UnsupportedDetail{
		Method: method,
		Path:   path,
	})
}

func UpstreamStatus(status int, kind string) *ErrorList {
	return ErrUpstream.WithDetail(UpstreamStatusDetail{
		Status: status,
		Kind:   kind,
	})
}

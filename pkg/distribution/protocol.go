package distribution

import "strings"

const (
	HeaderAccept                       = "Accept"
	HeaderAcceptRanges                 = "Accept-Ranges"
	HeaderAuthorization                = "Authorization"
	HeaderContentLength                = "Content-Length"
	HeaderContentRange                 = "Content-Range"
	HeaderContentType                  = "Content-Type"
	HeaderDockerContentDigest          = "Docker-Content-Digest"
	HeaderDockerDistributionAPIVersion = "Docker-Distribution-Api-Version"
	HeaderETag                         = "ETag"
	HeaderLink                         = "Link"
	HeaderLocation                     = "Location"
	HeaderRange                        = "Range"
	HeaderWarning                      = "Warning"
	HeaderWWWAuthenticate              = "WWW-Authenticate"
	HeaderUserAgent                    = "User-Agent"

	MediaTypeJSON        = "application/json"
	MediaTypeOctetStream = "application/octet-stream"

	RangeUnitBytes = "bytes"

	AuthSchemeBearer = "Bearer"

	WarningResponseIsStale = `110 - "Response is stale"`
)

var DefaultManifestAccept = strings.Join([]string{
	MediaTypeOCIIndex,
	MediaTypeDockerManifestList,
	MediaTypeOCIManifest,
	MediaTypeDockerManifest,
}, ", ")

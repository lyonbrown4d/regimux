package upstream

import (
	"errors"
	"net/http"

	"github.com/arcgolabs/clientx"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func shouldFailover(req failoverRequest, err error) bool {
	if err == nil {
		return false
	}

	if statusErr, ok := errors.AsType[*upstreamHTTPStatusError](err); ok {
		return shouldFailoverStatus(req, statusErr.status)
	}

	list := distribution.FromError(err)
	if list == nil {
		return shouldFailoverError(err)
	}
	return shouldFailoverStatus(req, list.Status)
}

func shouldFailoverError(err error) bool {
	switch clientx.KindOf(err) {
	case clientx.ErrorKindTimeout, clientx.ErrorKindTemporary, clientx.ErrorKindConnRefused, clientx.ErrorKindDNS, clientx.ErrorKindNetwork:
		return true
	case clientx.ErrorKindUnknown, clientx.ErrorKindCanceled, clientx.ErrorKindTLS, clientx.ErrorKindClosed, clientx.ErrorKindCodec:
		return false
	default:
		return false
	}
}

func shouldFailoverStatus(req failoverRequest, status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return false
	case http.StatusNotFound:
		return shouldFailoverNotFound(req.operation)
	case http.StatusTooManyRequests:
		return true
	default:
		return status >= http.StatusInternalServerError
	}
}

func shouldFailoverNotFound(operation string) bool {
	switch operation {
	case operationManifest, operationBlob, operationTags, operationReferrers:
		return true
	default:
		return false
	}
}

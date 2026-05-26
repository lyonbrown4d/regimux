// Package distribution contains Docker Distribution API response helpers.
package distribution

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const APIVersion = "registry/2.0"

type ErrorCode string

const (
	CodeUnknown         ErrorCode = "UNKNOWN"
	CodeUnsupported     ErrorCode = "UNSUPPORTED"
	CodeUnauthorized    ErrorCode = "UNAUTHORIZED"
	CodeDenied          ErrorCode = "DENIED"
	CodeTooManyRequests ErrorCode = "TOOMANYREQUESTS"
	CodeNameInvalid     ErrorCode = "NAME_INVALID"
	CodeNameUnknown     ErrorCode = "NAME_UNKNOWN"
	CodeManifestUnknown ErrorCode = "MANIFEST_UNKNOWN"
	CodeBlobUnknown     ErrorCode = "BLOB_UNKNOWN"
	CodeDigestInvalid   ErrorCode = "DIGEST_INVALID"
	CodeRangeInvalid    ErrorCode = "RANGE_INVALID"
	CodeUpstreamError   ErrorCode = "UPSTREAM_ERROR"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Detail  any       `json:"detail,omitempty"`
}

func (e Error) Error() string {
	return e.Message
}

type ErrorDescriptor struct {
	Status  int
	Code    ErrorCode
	Message string
}

func (e ErrorDescriptor) Error() string {
	return e.Message
}

func (e ErrorDescriptor) WithDetail(detail any) *ErrorList {
	return NewError(e.Status, e.Code, e.Message, detail)
}

func (e ErrorDescriptor) WithMessage(message string) *ErrorList {
	if message == "" {
		message = e.Message
	}
	return NewError(e.Status, e.Code, message, nil)
}

type ErrorResponse struct {
	Errors []Error `json:"errors"`
}

type ErrorList struct {
	Status int
	Errors []Error
}

func (e *ErrorList) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return string(CodeUnknown)
	}
	return e.Errors[0].Message
}

func NewError(status int, code ErrorCode, message string, detail any) *ErrorList {
	return &ErrorList{
		Status: status,
		Errors: []Error{
			{
				Code:    code,
				Message: message,
				Detail:  detail,
			},
		},
	}
}

var (
	ErrUnknown         = ErrorDescriptor{Status: http.StatusInternalServerError, Code: CodeUnknown, Message: "unknown error"}
	ErrUnsupported     = ErrorDescriptor{Status: http.StatusMethodNotAllowed, Code: CodeUnsupported, Message: "unsupported"}
	ErrUnauthorized    = ErrorDescriptor{Status: http.StatusUnauthorized, Code: CodeUnauthorized, Message: "authentication required"}
	ErrDenied          = ErrorDescriptor{Status: http.StatusForbidden, Code: CodeDenied, Message: "requested access to the resource is denied"}
	ErrTooManyRequests = ErrorDescriptor{Status: http.StatusTooManyRequests, Code: CodeTooManyRequests, Message: "too many requests"}
	ErrNameInvalid     = ErrorDescriptor{Status: http.StatusBadRequest, Code: CodeNameInvalid, Message: "invalid repository name"}
	ErrNameUnknown     = ErrorDescriptor{Status: http.StatusNotFound, Code: CodeNameUnknown, Message: "repository name not known to registry"}
	ErrManifestUnknown = ErrorDescriptor{Status: http.StatusNotFound, Code: CodeManifestUnknown, Message: "manifest unknown"}
	ErrBlobUnknown     = ErrorDescriptor{Status: http.StatusNotFound, Code: CodeBlobUnknown, Message: "blob unknown"}
	ErrDigestInvalid   = ErrorDescriptor{Status: http.StatusBadRequest, Code: CodeDigestInvalid, Message: "digest invalid"}
	ErrDigestMismatch  = ErrorDescriptor{Status: http.StatusBadGateway, Code: CodeDigestInvalid, Message: "digest invalid"}
	ErrRangeInvalid    = ErrorDescriptor{Status: http.StatusRequestedRangeNotSatisfiable, Code: CodeRangeInvalid, Message: "invalid range"}
	ErrUpstream        = ErrorDescriptor{Status: http.StatusBadGateway, Code: CodeUpstreamError, Message: "upstream error"}
)

var statusByCode = map[ErrorCode]int{
	CodeUnknown:         http.StatusInternalServerError,
	CodeUnsupported:     http.StatusMethodNotAllowed,
	CodeUnauthorized:    http.StatusUnauthorized,
	CodeDenied:          http.StatusForbidden,
	CodeTooManyRequests: http.StatusTooManyRequests,
	CodeNameInvalid:     http.StatusBadRequest,
	CodeNameUnknown:     http.StatusNotFound,
	CodeManifestUnknown: http.StatusNotFound,
	CodeBlobUnknown:     http.StatusNotFound,
	CodeDigestInvalid:   http.StatusBadRequest,
	CodeRangeInvalid:    http.StatusRequestedRangeNotSatisfiable,
	CodeUpstreamError:   http.StatusBadGateway,
}

func ManifestUnknown(repo, reference string) *ErrorList {
	return ErrManifestUnknown.WithDetail(map[string]string{
		"repo":      repo,
		"reference": reference,
	})
}

func BlobUnknown(repo, digest string) *ErrorList {
	return ErrBlobUnknown.WithDetail(map[string]string{
		"repo":   repo,
		"digest": digest,
	})
}

func FromError(err error) *ErrorList {
	if err == nil {
		return nil
	}

	var list *ErrorList
	if errors.As(err, &list) {
		return list
	}

	var distErr Error
	if errors.As(err, &distErr) {
		return &ErrorList{
			Status: defaultStatus(distErr.Code),
			Errors: []Error{distErr},
		}
	}

	var descriptor ErrorDescriptor
	if errors.As(err, &descriptor) {
		return descriptor.WithDetail(nil)
	}

	return NewError(http.StatusInternalServerError, CodeUnknown, "unknown error", err.Error())
}

func WriteError(w http.ResponseWriter, err error) {
	list := FromError(err)
	if list == nil {
		list = NewError(http.StatusInternalServerError, CodeUnknown, "unknown error", nil)
	}
	status := list.Status
	if status == 0 {
		status = defaultStatus(list.Errors[0].Code)
	}

	header := w.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Docker-Distribution-Api-Version", APIVersion)
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(ErrorResponse{Errors: list.Errors}); err != nil {
		return
	}
}

func WriteDistributionError(w http.ResponseWriter, err error) {
	WriteError(w, err)
}

func MarshalError(err error) ([]byte, error) {
	list := FromError(err)
	if list == nil {
		list = NewError(http.StatusInternalServerError, CodeUnknown, "unknown error", nil)
	}
	body, err := json.Marshal(ErrorResponse{Errors: list.Errors})
	if err != nil {
		return nil, fmt.Errorf("marshal distribution error response: %w", err)
	}
	return body, nil
}

func defaultStatus(code ErrorCode) int {
	if status, ok := statusByCode[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

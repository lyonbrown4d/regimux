package auth

import (
	"github.com/arcgolabs/authx"
	authxhttp "github.com/arcgolabs/authx/http"
	"github.com/samber/oops"
)

func newAuthError(code, message string, fields ...any) error {
	classification := authx.ClassificationForCode(code)
	return classifiedAuthError(classification, fields...).Errorf("%s", message)
}

func wrapAuthError(err error, fallbackCode, message string, fields ...any) error {
	if err == nil {
		return newAuthError(fallbackCode, message, fields...)
	}
	classification := authx.ClassifyError(err)
	if classification.Code == authx.ErrorCodeInternal && fallbackCode != "" {
		classification = authx.ClassificationForCode(fallbackCode)
	}
	return classifiedAuthError(classification, fields...).Wrapf(err, "%s", message)
}

func newHTTPAuthError(code, message string, fields ...any) error {
	classification := authxhttp.ClassificationForCode(code)
	fields = append(fields, "http_status", authxhttp.StatusCodeFromClassification(classification))
	return classifiedAuthError(classification, fields...).Errorf("%s", message)
}

func classifiedAuthError(classification authx.ErrorClassification, fields ...any) oops.OopsErrorBuilder {
	fields = append(fields, classification.OopsFields()...)
	return oops.In("auth").
		Code(classification.Code).
		Public(classification.SafeMessage).
		With(fields...)
}

package object

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	minS3PartSize            int64 = 5 * 1024 * 1024
	defaultS3PartSize        int64 = 16 * 1024 * 1024
	maxS3PartSize            int64 = 5 * 1024 * 1024 * 1024
	maxS3UploadParts               = 10_000
	defaultUploadConcurrency       = 4
)

func normalizeS3Options(options S3Options) (S3Options, error) {
	options.Bucket = strings.TrimSpace(options.Bucket)
	options.Prefix = normalizeObjectPrefix(options.Prefix)
	options.Region = strings.TrimSpace(options.Region)
	options.Endpoint = strings.TrimRight(strings.TrimSpace(options.Endpoint), "/")
	options.AccessKeyID = strings.TrimSpace(options.AccessKeyID)
	options.SecretAccessKey = strings.TrimSpace(options.SecretAccessKey)
	options.SessionToken = strings.TrimSpace(options.SessionToken)
	options.Profile = strings.TrimSpace(options.Profile)

	if options.Bucket == "" {
		return S3Options{}, errors.New("S3 bucket is required")
	}
	if options.Region == "" {
		options.Region = "us-east-1"
	}
	if err := validateS3Credentials(options); err != nil {
		return S3Options{}, err
	}

	return normalizeS3UploadOptions(options)
}

func validateS3Credentials(options S3Options) error {
	if (options.AccessKeyID == "") != (options.SecretAccessKey == "") {
		return errors.New("S3 access key ID and secret access key must be configured together")
	}
	if options.SessionToken != "" && options.AccessKeyID == "" {
		return errors.New("S3 session token requires static credentials")
	}

	return nil
}

func normalizeS3UploadOptions(options S3Options) (S3Options, error) {
	if options.PartSize == 0 {
		options.PartSize = defaultS3PartSize
	}
	if options.PartSize < minS3PartSize || options.PartSize > maxS3PartSize {
		return S3Options{}, fmt.Errorf(
			"S3 part size must be between %d and %d bytes",
			minS3PartSize,
			maxS3PartSize,
		)
	}
	if options.PartSize >= int64(^uint(0)>>1) {
		return S3Options{}, fmt.Errorf("S3 part size %d exceeds platform capacity", options.PartSize)
	}
	if options.UploadConcurrency == 0 {
		options.UploadConcurrency = defaultUploadConcurrency
	}
	if options.UploadConcurrency < 0 {
		return S3Options{}, errors.New("S3 upload concurrency cannot be negative")
	}

	return options, nil
}

func loadAWSConfig(ctx context.Context, options S3Options) (aws.Config, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(options.Region),
	}
	if options.Profile != "" {
		loadOptions = append(loadOptions, awsconfig.WithSharedConfigProfile(options.Profile))
	}
	if options.AccessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				options.AccessKeyID,
				options.SecretAccessKey,
				options.SessionToken,
			),
		))
	}

	config, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load AWS configuration: %w", err)
	}

	return config, nil
}

func wrapS3OperationError(operation, digest string, err error) error {
	if isS3NotFound(err) {
		return fmt.Errorf("%s S3 object %q: %w", operation, digest, ErrNotFound)
	}

	return fmt.Errorf("%s S3 object %q: %w", operation, digest, err)
}

func isS3NotFound(err error) bool {
	var responseError *smithyhttp.ResponseError
	if errors.As(err, &responseError) &&
		responseError.HTTPStatusCode() == http.StatusNotFound {
		return true
	}

	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}

	switch strings.ToLower(apiError.ErrorCode()) {
	case "nosuchkey", "notfound":
		return true
	default:
		return false
	}
}

func normalizeETag(etag string) string {
	return strings.Trim(etag, "\"")
}

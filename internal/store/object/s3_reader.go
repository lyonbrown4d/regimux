package object

import (
	"context"
	"fmt"
	"io"
	"os"
)

type s3SeekableReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type immutableFileReader struct{ *os.File }

// ImmutableFileReader marks a privately owned, completed cache spool for reuse.
// The caller must not modify its file until Put returns and retains ownership of
// closing it. Ordinary readers and files are copied into a verified snapshot.
func ImmutableFileReader(file *os.File) io.Reader {
	return immutableFileReader{File: file}
}

func (s *S3Store) uploadSeekable(
	ctx context.Context,
	digest, key string,
	body s3SeekableReader,
	opts PutOptions,
) (*s3UploadResult, error) {
	offset, err := body.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("read S3 upload position: %w", err)
	}
	verifier := newDigestVerifier(digest)
	if err := copyS3Upload(verifier, &contextReader{ctx: ctx, reader: body}, s.maxUploadSize()); err != nil {
		return nil, err
	}
	if err := verifier.verify(); err != nil {
		return nil, err
	}
	// Cache spools already implement ReaderAt: reuse them without another copy.
	section := io.NewSectionReader(body, offset, verifier.sizeValue())
	return s.uploadVerified(ctx, key, section, verifier, opts)
}

func (s *S3Store) uploadVerified(
	ctx context.Context,
	key string,
	body io.ReaderAt,
	verifier *digestVerifier,
	opts PutOptions,
) (*s3UploadResult, error) {
	if verifier.sizeValue() <= s.partSize {
		return s.uploadSingle(ctx, key, io.NewSectionReader(body, 0, verifier.sizeValue()), verifier, opts)
	}
	if (verifier.sizeValue()-1)/s.partSize >= maxS3UploadParts {
		return nil, fmt.Errorf("S3 multipart upload exceeds %d parts", maxS3UploadParts)
	}
	return s.uploadMultipart(ctx, key, body, verifier, opts)
}

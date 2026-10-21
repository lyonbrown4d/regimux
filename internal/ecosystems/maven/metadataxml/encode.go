// Package metadataxml streams Maven metadata through a bounded, cancelable XML encoder.
package metadataxml

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

var ErrOutputTooLarge = errors.New("maven metadata output exceeds its byte limit")

func Encode(ctx context.Context, destination io.Writer, value any, limit int64) (int64, error) {
	writer := &budgetWriter{ctx: ctx, destination: destination, limit: limit}
	if _, err := io.WriteString(writer, xml.Header); err != nil {
		return writer.written, fmt.Errorf("write Maven metadata XML header: %w", err)
	}
	encoder := xml.NewEncoder(writer)
	encoder.Indent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return writer.written, fmt.Errorf("encode Maven metadata XML: %w", err)
	}
	return writer.written, nil
}

type budgetWriter struct {
	ctx         context.Context
	destination io.Writer
	limit       int64
	written     int64
}

func (writer *budgetWriter) Write(data []byte) (int, error) {
	if err := writer.ctx.Err(); err != nil {
		return 0, fmt.Errorf("write Maven metadata XML: %w", err)
	}
	if int64(len(data)) > writer.limit-writer.written {
		return 0, fmt.Errorf("maven group metadata output exceeds %d bytes: %w", writer.limit, ErrOutputTooLarge)
	}
	written, err := writer.destination.Write(data)
	writer.written += int64(written)
	if err != nil {
		return written, fmt.Errorf("write Maven metadata XML: %w", err)
	}
	if written != len(data) {
		return written, fmt.Errorf("write Maven metadata XML: %w", io.ErrShortWrite)
	}
	if err := writer.ctx.Err(); err != nil {
		return written, fmt.Errorf("write Maven metadata XML: %w", err)
	}
	return written, nil
}

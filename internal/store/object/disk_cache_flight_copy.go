package object

import (
	"errors"
	"fmt"
	"io"

	ocidigest "github.com/opencontainers/go-digest"
)

func copyVerifiedDiskCacheStream(fill *diskCacheFill, verifier ocidigest.Verifier) error {
	buffer := make([]byte, 32*1024)
	if fill.info.Size == 0 {
		return verifyDiskCacheFlightComplete(fill, verifier, 0)
	}
	return copyDiskCacheFlightToSize(fill, verifier, buffer)
}

func copyDiskCacheFlightToSize(fill *diskCacheFill, verifier ocidigest.Verifier, buffer []byte) error {
	var total int64
	noProgress := 0
	for total < fill.info.Size {
		if err := fill.ctx.Err(); err != nil {
			return fmt.Errorf("read canonical object: %w", err)
		}
		n, readErr := readAndVerifyDiskCacheFlightChunk(fill, verifier, buffer, fill.info.Size-total)
		total += int64(n)
		if done, err := finishDiskCacheFlightRead(fill, verifier, total, readErr); done {
			return err
		}
		if n == 0 {
			noProgress++
		} else {
			noProgress = 0
		}
		if noProgress == 100 {
			return fmt.Errorf("read canonical object: %w", io.ErrNoProgress)
		}
	}
	return verifyDiskCacheFlightComplete(fill, verifier, total)
}

func finishDiskCacheFlightRead(
	fill *diskCacheFill,
	verifier ocidigest.Verifier,
	total int64,
	readErr error,
) (bool, error) {
	if errors.Is(readErr, io.EOF) {
		return true, verifyDiskCacheFlightComplete(fill, verifier, total)
	}
	if readErr != nil {
		return true, fmt.Errorf("read canonical object: %w", readErr)
	}
	if total == fill.info.Size {
		return true, verifyDiskCacheFlightComplete(fill, verifier, total)
	}
	return false, nil
}

func readAndVerifyDiskCacheFlightChunk(
	fill *diskCacheFill,
	verifier ocidigest.Verifier,
	buffer []byte,
	remaining int64,
) (int, error) {
	readBuffer := diskCacheRemainingBuffer(buffer, remaining)
	n, err := fill.source.Read(readBuffer)
	if n > 0 {
		finalChunk := int64(n) == remaining
		if writeErr := writeVerifiedDiskCacheChunk(fill, verifier, readBuffer[:n], finalChunk); writeErr != nil {
			return n, writeErr
		}
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read canonical object: %w", err)
	}
	return n, nil
}

func diskCacheRemainingBuffer(buffer []byte, remaining int64) []byte {
	if remaining < int64(len(buffer)) {
		return buffer[:int(remaining)]
	}
	return buffer
}

func writeVerifiedDiskCacheChunk(fill *diskCacheFill, verifier ocidigest.Verifier, chunk []byte, final bool) error {
	written, err := verifier.Write(chunk)
	if err != nil {
		return fmt.Errorf("verify canonical object: %w", err)
	}
	if written != len(chunk) {
		return fmt.Errorf("verify canonical object: %w", io.ErrShortWrite)
	}
	if final && !verifier.Verified() {
		return ErrDigestMismatch
	}
	written, err = fill.stream.Write(chunk)
	if err != nil {
		return fmt.Errorf("write disk cache stream: %w", err)
	}
	if written != len(chunk) {
		return fmt.Errorf("write disk cache stream: %w", io.ErrShortWrite)
	}
	return nil
}

func verifyDiskCacheFlightComplete(fill *diskCacheFill, verifier ocidigest.Verifier, total int64) error {
	if total != fill.info.Size || !verifier.Verified() {
		return ErrDigestMismatch
	}
	return nil
}

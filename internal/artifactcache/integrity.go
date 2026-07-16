package artifactcache

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/net/html/charset"
)

func ValidateBody(
	body io.ReaderAt,
	size int64,
	headers http.Header,
	validator BodyValidator,
) error {
	if body == nil || size == 0 {
		return errEmptyBody
	}
	if err := validateDeclaredContentLength(headers, size); err != nil {
		return err
	}
	if validator == nil {
		return nil
	}
	if err := validator(body, size); err != nil {
		return fmt.Errorf("validate artifact content: %w", err)
	}
	return nil
}

func validateDeclaredContentLength(headers http.Header, actual int64) error {
	raw := strings.TrimSpace(headers.Get("Content-Length"))
	if raw == "" {
		return nil
	}
	declared, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || declared < 0 {
		return fmt.Errorf("invalid artifact content length %q", raw)
	}
	if declared != actual {
		return fmt.Errorf(
			"artifact content length mismatch: declared %d bytes, received %d bytes",
			declared,
			actual,
		)
	}
	return nil
}

func ValidateXML(body io.ReaderAt, size int64) error {
	return validateXMLRoot(body, size, nil)
}

func ValidateXMLRoot(body io.ReaderAt, size int64, roots ...string) error {
	allowed := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root != "" {
			allowed[root] = struct{}{}
		}
	}
	return validateXMLRoot(body, size, allowed)
}

func XMLRootValidator(roots ...string) BodyValidator {
	return func(body io.ReaderAt, size int64) error {
		return ValidateXMLRoot(body, size, roots...)
	}
}

func validateXMLRoot(body io.ReaderAt, size int64, allowed map[string]struct{}) error {
	if body == nil || size <= 0 {
		return errEmptyBody
	}
	decoder := xml.NewDecoder(io.NewSectionReader(body, 0, size))
	decoder.CharsetReader = charset.NewReaderLabel
	root, err := decodeXMLRoot(decoder)
	if err != nil {
		return err
	}
	if len(allowed) == 0 {
		return nil
	}
	if _, ok := allowed[root]; !ok {
		return fmt.Errorf("unexpected XML root element %q", root)
	}
	return nil
}

func decodeXMLRoot(decoder *xml.Decoder) (string, error) {
	root, err := readXMLRoot(decoder)
	if err != nil {
		return "", err
	}
	if err := consumeXML(decoder); err != nil {
		return "", err
	}
	return root, nil
}

func readXMLRoot(decoder *xml.Decoder) (string, error) {
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return "", errors.New("XML document has no root element")
		}
		if err != nil {
			return "", fmt.Errorf("decode XML document: %w", err)
		}
		if start, ok := token.(xml.StartElement); ok {
			return start.Name.Local, nil
		}
	}
}

func consumeXML(decoder *xml.Decoder) error {
	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode XML document: %w", err)
		}
	}
}

func ValidateZIP(body io.ReaderAt, size int64) error {
	if body == nil || size <= 0 {
		return errEmptyBody
	}
	if _, err := zip.NewReader(body, size); err != nil {
		return fmt.Errorf("open ZIP archive: %w", err)
	}
	return nil
}

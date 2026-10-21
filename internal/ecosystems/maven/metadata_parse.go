package maven

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/html/charset"
)

var errMavenMetadataStructureLimit = errors.New("maven metadata XML structure limit exceeded")

type metadataParseLimits struct {
	maxElements   int
	maxDepth      int
	maxAttributes int
	maxTokenBytes int
}

func parseMavenMetadata(data []byte) (mavenMetadataDocument, error) {
	return parseMavenMetadataWithLimits(data, metadataParseLimits{maxElements: 100_000, maxDepth: 128})
}

func parseMavenMetadataWithLimits(data []byte, limits metadataParseLimits) (mavenMetadataDocument, error) {
	if limits.maxAttributes <= 0 {
		limits.maxAttributes = 100_000
	}
	if limits.maxTokenBytes <= 0 {
		limits.maxTokenBytes = 4 << 20
	}
	input := &metadataTokenInput{reader: bytes.NewReader(data)}
	source := xml.NewDecoder(input)
	source.CharsetReader = charset.NewReaderLabel
	decoder := xml.NewTokenDecoder(&metadataXMLBudgetReader{source: source, input: input, limits: limits})
	var document mavenMetadataDocument
	if err := decoder.Decode(&document); err != nil {
		return mavenMetadataDocument{}, fmt.Errorf("unmarshal Maven metadata: %w", err)
	}
	if document.XMLName.Local != "metadata" {
		return mavenMetadataDocument{}, fmt.Errorf("unexpected maven metadata root %q", document.XMLName.Local)
	}
	return document, nil
}

type metadataXMLBudgetReader struct {
	source     *xml.Decoder
	input      *metadataTokenInput
	limits     metadataParseLimits
	elements   int
	attributes int
	depth      int
}

func (reader *metadataXMLBudgetReader) Token() (xml.Token, error) {
	// RawToken preserves the original prefixes; the outer standard decoder
	// performs namespace resolution and validates matching start/end elements.
	reader.input.remaining = reader.limits.maxTokenBytes
	token, err := reader.source.RawToken()
	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, fmt.Errorf("read Maven metadata XML token: %w", err)
	}
	switch value := token.(type) {
	case xml.StartElement:
		reader.elements++
		reader.depth++
		reader.attributes += len(value.Attr)
		if err := reader.checkStructure(); err != nil {
			return nil, err
		}
	case xml.EndElement:
		reader.depth--
	}
	return token, nil
}

func (reader *metadataXMLBudgetReader) checkStructure() error {
	if reader.elements > reader.limits.maxElements || reader.depth > reader.limits.maxDepth || reader.attributes > reader.limits.maxAttributes {
		return fmt.Errorf("%w: elements=%d/%d depth=%d/%d attributes=%d/%d", errMavenMetadataStructureLimit,
			reader.elements, reader.limits.maxElements, reader.depth, reader.limits.maxDepth,
			reader.attributes, reader.limits.maxAttributes)
	}
	return nil
}

package maven

import (
	"bytes"
	"encoding/xml"
	"errors"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html/charset"
)

func TestMavenMetadataParseBoundsEmptyElementAmplification(t *testing.T) {
	body := []byte("<metadata><plugins>" + strings.Repeat("<plugin/>", 5) + "</plugins></metadata>")
	limits := metadataParseLimits{maxElements: 7, maxDepth: 3}
	document, err := parseMavenMetadataWithLimits(body, limits)
	if err != nil || document.Plugins == nil || len(document.Plugins.Items) != 5 {
		t.Fatalf("parse at exact element limit: document=%+v error=%v", document, err)
	}
	limits.maxElements--
	if _, err := parseMavenMetadataWithLimits(body, limits); !errors.Is(err, errMavenMetadataStructureLimit) {
		t.Fatalf("parse above element limit: %v", err)
	}
}

func TestMavenMetadataParseBoundsDepthThroughUnknownElements(t *testing.T) {
	limits := metadataParseLimits{maxElements: 100, maxDepth: 3}
	if _, err := parseMavenMetadataWithLimits([]byte("<metadata><extra><nested/></extra></metadata>"), limits); err != nil {
		t.Fatalf("parse at exact depth limit: %v", err)
	}
	body := []byte("<metadata><extra><nested><deeper/></nested></extra></metadata>")
	if _, err := parseMavenMetadataWithLimits(body, limits); !errors.Is(err, errMavenMetadataStructureLimit) {
		t.Fatalf("parse above depth limit through unknown fields: %v", err)
	}
}

func TestMavenMetadataParsePreservesStandardNamespacesAndCharset(t *testing.T) {
	body := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><m:metadata xmlns:m=\"urn:maven\"><m:groupId>caf\xe9</m:groupId></m:metadata>")
	standard := xml.NewDecoder(bytes.NewReader(body))
	standard.CharsetReader = charset.NewReaderLabel
	var expected mavenMetadataDocument
	if err := standard.Decode(&expected); err != nil {
		t.Fatal(err)
	}
	actual, err := parseMavenMetadata(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) || actual.GroupID != "café" {
		t.Fatalf("bounded parser changed decoded metadata: actual=%+v expected=%+v", actual, expected)
	}
}

func TestMavenMetadataParseRejectsMismatchedElements(t *testing.T) {
	if _, err := parseMavenMetadata([]byte("<metadata><groupId>broken</artifactId></metadata>")); err == nil {
		t.Fatal("bounded parser accepted mismatched XML elements")
	}
}

func TestMavenMetadataParseBoundsStartTagBeforeAllAttributesAreDecoded(t *testing.T) {
	body := []byte("<metadata " + strings.Repeat(`a="" `, 200) + "></metadata>")
	limits := metadataParseLimits{maxElements: 100, maxDepth: 10, maxTokenBytes: 128}
	if _, err := parseMavenMetadataWithLimits(body, limits); !errors.Is(err, errMavenMetadataStructureLimit) {
		t.Fatalf("oversized start tag error = %v", err)
	}
}

func TestMavenMetadataParseBoundsCumulativeAttributes(t *testing.T) {
	body := []byte(`<metadata a=""><extra b=""/></metadata>`)
	limits := metadataParseLimits{maxElements: 100, maxDepth: 10, maxAttributes: 2}
	if _, err := parseMavenMetadataWithLimits(body, limits); err != nil {
		t.Fatalf("exact attribute limit rejected: %v", err)
	}
	limits.maxAttributes--
	if _, err := parseMavenMetadataWithLimits(body, limits); !errors.Is(err, errMavenMetadataStructureLimit) {
		t.Fatalf("cumulative attribute budget error = %v", err)
	}
}

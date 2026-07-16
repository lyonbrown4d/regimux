package maven

import "testing"

func TestParseMavenMetadataAcceptsLegacyCharset(t *testing.T) {
	body := append(
		[]byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><metadata><groupId>org.example."),
		0xe9,
	)
	body = append(body, []byte("</groupId><artifactId>demo</artifactId></metadata>")...)

	document, err := parseMavenMetadata(body)
	if err != nil {
		t.Fatalf("parse Maven metadata: %v", err)
	}
	if document.GroupID != "org.example.\u00e9" {
		t.Fatalf("group ID = %q, want %q", document.GroupID, "org.example.\u00e9")
	}
}

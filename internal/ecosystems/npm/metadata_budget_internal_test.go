package npm

import (
	"errors"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

func TestRewriteMetadataChecksStructureBeforeDecodingUnrelatedFields(t *testing.T) {
	body := []byte(`{"versions":{},"extra":` + strings.Repeat("[", npmMetadataMaxNestingDepth) +
		`null` + strings.Repeat("]", npmMetadataMaxNestingDepth) + `}`)
	if _, _, err := rewriteMetadata(body, "", "", ""); !errors.Is(err, jsonbudget.ErrLimit) {
		t.Fatalf("rewrite with over-budget unrelated field error = %v", err)
	}
}

func TestRewriteMetadataRejectsInvalidUTF8BeforeDecoding(t *testing.T) {
	body := []byte("{\"versions\":{},\"extra\":\"\xff\"}")
	if _, _, err := rewriteMetadata(body, "", "", ""); !errors.Is(err, jsonbudget.ErrInvalidJSON) {
		t.Fatalf("rewrite with invalid UTF-8 error = %v", err)
	}
}

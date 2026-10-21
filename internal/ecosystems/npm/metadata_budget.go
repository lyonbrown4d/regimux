package npm

import (
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
)

const (
	npmMetadataMaxStructureMarkers = 1_000_000
	npmMetadataMaxNestingDepth     = 128
	npmMetadataMaxStringBytes      = 32 << 20
	npmMetadataMaxLiteralBytes     = 1 << 10
)

func checkNPMMetadataShape(body []byte) error {
	if err := jsonbudget.Check(body, jsonbudget.Limits{
		MaxStructuralMarkers: npmMetadataMaxStructureMarkers,
		MaxDepth:             npmMetadataMaxNestingDepth,
		MaxStringBytes:       npmMetadataMaxStringBytes,
		MaxLiteralBytes:      npmMetadataMaxLiteralBytes,
	}); err != nil {
		return fmt.Errorf("check npm metadata structure: %w", err)
	}
	return nil
}

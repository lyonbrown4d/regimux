package prefetch

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/samber/lo"
)

type versionTag struct {
	raw      string
	prefix   string
	segments int
	version  *semver.Version
	suffix   string
}

func parseAvailableTags(tags *collectionlist.List[string]) *collectionlist.List[versionTag] {
	if tags == nil {
		return collectionlist.NewList[versionTag]()
	}
	return collectionlist.NewList(lo.FilterMap(lo.Uniq(tags.Values()), func(tag string, _ int) (versionTag, bool) {
		return parseVersionTag(tag)
	})...)
}

func parseVersionTag(tag string) (versionTag, bool) {
	raw := strings.TrimSpace(tag)
	if raw == "" || strings.EqualFold(raw, "latest") {
		return versionTag{}, false
	}

	clean, prefix := normalizeTagPrefix(raw)
	parts := strings.SplitN(clean, "-", 2)
	if strings.TrimSpace(parts[0]) == "" {
		return versionTag{}, false
	}

	base := parts[0]
	normalizedBase, segments, ok := normalizeSemverBase(base)
	if !ok {
		return versionTag{}, false
	}

	suffix := ""
	if len(parts) == 2 {
		suffix = strings.TrimSpace(parts[1])
		if suffix == "" {
			return versionTag{}, false
		}
	}

	versionText := normalizedBase
	if suffix != "" {
		versionText += "-" + normalizeSemverSuffix(suffix)
	}
	parsed, err := semver.NewVersion(versionText)
	if err != nil {
		return versionTag{}, false
	}

	return versionTag{
		raw:      raw,
		prefix:   prefix,
		segments: segments,
		version:  parsed,
		suffix:   suffix,
	}, true
}

func isCompatibleCandidate(source, target versionTag, maxDistance int) bool {
	if target.raw == source.raw {
		return false
	}
	if !strings.EqualFold(source.prefix, target.prefix) {
		return false
	}
	if source.suffix != target.suffix {
		return false
	}
	if source.segments > target.segments {
		return false
	}
	if compareVersionSegments(source.version, target.version, source.segments) >= 0 {
		return false
	}

	distance := versionDistance(source, target)
	return distance > 0 && distance <= maxDistance
}

func normalizeTagPrefix(raw string) (string, string) {
	prefix := ""
	if strings.HasPrefix(raw, "v") || strings.HasPrefix(raw, "V") {
		prefix = raw[:1]
		raw = raw[1:]
	}
	return raw, prefix
}

func normalizeSemverBase(raw string) (string, int, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return "", 0, false
	}

	if slices.Contains(parts, "") {
		return "", 0, false
	}
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return "", 0, false
	}
	segments := len(parts)
	if missing := 3 - segments; missing > 0 {
		normalized += strings.Repeat(".0", missing)
	}
	return normalized, segments, true
}

func normalizeSemverSuffix(raw string) string {
	return strings.ReplaceAll(raw, "_", "-")
}

func compareVersionSegments(left, right *semver.Version, segments int) int {
	for i := range segments {
		leftSegment := getVersionSegment(left, i)
		rightSegment := getVersionSegment(right, i)
		if leftSegment < rightSegment {
			return -1
		}
		if leftSegment > rightSegment {
			return 1
		}
	}
	return 0
}

func getVersionSegment(version *semver.Version, index int) int {
	if version == nil {
		return 0
	}
	switch index {
	case 0:
		return safeUint64ToInt(version.Major())
	case 1:
		return safeUint64ToInt(version.Minor())
	case 2:
		return safeUint64ToInt(version.Patch())
	default:
		return 0
	}
}

func versionDistance(source, target versionTag) int {
	for i := range source.segments {
		sourceSegment := getVersionSegment(source.version, i)
		targetSegment := getVersionSegment(target.version, i)
		if sourceSegment != targetSegment {
			return targetSegment - sourceSegment
		}
	}
	return 0
}

func compareTagNames(left, right string) int {
	leftVersion, leftOK := parseVersionTag(left)
	rightVersion, rightOK := parseVersionTag(right)
	if leftOK && rightOK {
		if versionCompare := compareVersionSegments(leftVersion.version, rightVersion.version, minVersionSegments(leftVersion, rightVersion)); versionCompare != 0 {
			return versionCompare
		}
		if leftVersion.suffix < rightVersion.suffix {
			return -1
		}
		if leftVersion.suffix > rightVersion.suffix {
			return 1
		}
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func minVersionSegments(left, right versionTag) int {
	if left.segments < right.segments {
		return left.segments
	}
	return right.segments
}

func safeUint64ToInt(value uint64) int {
	maxInt := int(^uint(0) >> 1)
	if value > uint64(maxInt) {
		return maxInt
	}
	out, err := strconv.Atoi(strconv.FormatUint(value, 10))
	if err != nil {
		return maxInt
	}
	return out
}

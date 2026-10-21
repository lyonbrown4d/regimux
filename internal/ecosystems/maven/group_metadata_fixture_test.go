package maven_test

import (
	"fmt"
	"strings"
)

// Keep each token below the parser read budget while exercising total input size.
func sizedCommentMetadata(size int) string {
	const prefix = "<metadata>"
	const suffix = "</metadata>"
	remaining := size - len(prefix) - len(suffix)
	parts := []string{prefix}
	for remaining >= len("<!---->") {
		content := min(remaining-len("<!---->"), 512<<10)
		parts = append(parts, "<!--"+strings.Repeat("x", content)+"-->")
		remaining -= content + len("<!---->")
	}
	parts = append(parts, strings.Repeat(" ", remaining), suffix)
	return strings.Join(parts, "")
}

// Distinct plugin keys retain all names during merging; indentation then pushes
// the near-limit compact input beyond the aggregate encoded-output budget.
func sizedPluginMetadata(size int) string {
	const prefix = "<metadata><plugins>"
	const suffix = "</plugins></metadata>"
	const namePrefix = "<plugin><name>"
	const plugins = 32
	endings := make([]string, plugins)
	remaining := size - len(prefix) - len(suffix)
	for index := range plugins {
		endings[index] = fmt.Sprintf("</name><prefix>p%02d</prefix><artifactId>a%02d</artifactId></plugin>", index, index)
		remaining -= len(namePrefix) + len(endings[index])
	}
	parts := make([]string, 0, plugins+2)
	parts = append(parts, prefix)
	for index := range plugins {
		content := remaining / (plugins - index)
		parts = append(parts, namePrefix+strings.Repeat("x", content)+endings[index])
		remaining -= content
	}
	parts = append(parts, suffix)
	return strings.Join(parts, "")
}

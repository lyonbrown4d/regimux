package distribution

import (
	"fmt"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
)

type ManifestUnknownDetail struct {
	Repository            string               `json:"repo"`
	Reference             string               `json:"reference"`
	Message               string               `json:"message,omitempty"`
	Suggestions           []ManifestSuggestion `json:"suggestions,omitempty"`
	RepositorySuggestions []ManifestSuggestion `json:"repository_suggestions,omitempty"`
}

type ManifestSuggestion struct {
	Reference string `json:"reference"`
	Image     string `json:"image,omitempty"`
}

type BlobUnknownDetail struct {
	Repository string `json:"repo"`
	Digest     string `json:"digest"`
}

type DigestMismatchDetail struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type UnsupportedDetail struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type UpstreamStatusDetail struct {
	Status int    `json:"status"`
	Kind   string `json:"kind,omitempty"`
}

func DigestMismatch(expected, actual string) *ErrorList {
	return ErrDigestMismatch.WithDetail(DigestMismatchDetail{
		Expected: expected,
		Actual:   actual,
	})
}

func Unsupported(method, path string) *ErrorList {
	return ErrUnsupported.WithDetail(UnsupportedDetail{
		Method: method,
		Path:   path,
	})
}

func UpstreamStatus(status int, kind string) *ErrorList {
	return ErrUpstream.WithDetail(UpstreamStatusDetail{
		Status: status,
		Kind:   kind,
	})
}

func ManifestUnknownWithSuggestions(alias, repo, reference string, tags, repositories []string) *ErrorList {
	message := manifestUnknownSuggestionMessage(repo, reference, tags, repositories)
	return NewError(ErrManifestUnknown.Status, CodeManifestUnknown, message, ManifestUnknownDetail{
		Repository:            repo,
		Reference:             reference,
		Message:               message,
		Suggestions:           manifestTagSuggestions(alias, repo, tags),
		RepositorySuggestions: manifestRepositorySuggestions(alias, reference, repositories),
	})
}

func manifestUnknownSuggestionMessage(repo, reference string, tags, repositories []string) string {
	if len(tags) == 0 && len(repositories) == 0 {
		return ErrManifestUnknown.Message
	}
	if len(tags) > 0 {
		suggestion := strings.Join(tags[:min(len(tags), 2)], " or ")
		return fmt.Sprintf("manifest unknown: tag %q not found for repository %q; did you mean %s?", reference, repo, suggestion)
	}
	suggestion := strings.Join(repositories[:min(len(repositories), 2)], " or ")
	return fmt.Sprintf("manifest unknown: repository %q or tag %q was not found; did you mean image %s?", repo, reference, suggestion)
}

func manifestTagSuggestions(alias, repo string, tags []string) []ManifestSuggestion {
	return collectionlist.MapList(collectionlist.NewList(tags...), func(_ int, tag string) ManifestSuggestion {
		return ManifestSuggestion{
			Reference: tag,
			Image:     suggestedImage(alias, repo, tag),
		}
	}).Values()
}

func manifestRepositorySuggestions(alias, reference string, repositories []string) []ManifestSuggestion {
	return collectionlist.MapList(collectionlist.NewList(repositories...), func(_ int, repo string) ManifestSuggestion {
		return ManifestSuggestion{
			Reference: reference,
			Image:     suggestedImage(alias, repo, reference),
		}
	}).Values()
}

func suggestedImage(alias, repo, tag string) string {
	name := strings.Trim(strings.Trim(alias, "/")+"/"+strings.Trim(repo, "/"), "/")
	if name == "" {
		return tag
	}
	return name + ":" + tag
}

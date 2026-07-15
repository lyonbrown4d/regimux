package suggestion

import (
	"context"
	"encoding/json"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"net/url"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
	"go.uber.org/multierr"
)

func (s *Service) fetchTagIndex(ctx context.Context, req ManifestRequest) (*TagIndex, error) {
	tags, err := s.fetchTags(ctx, req)
	if err != nil {
		return nil, err
	}
	return &TagIndex{
		Alias:      req.Alias,
		Repository: req.Repository,
		Tags:       tags,
		FetchedAt:  utcNow(),
	}, nil
}

func (s *Service) fetchTags(ctx context.Context, req ManifestRequest) ([]string, error) {
	var tags []string
	last := ""
	for range s.opts.MaxTagPages {
		page, nextLast, err := s.fetchTagPage(ctx, req, last)
		if err != nil {
			return nil, err
		}
		tags = append(tags, page...)
		if nextLast == "" || nextLast == last {
			break
		}
		last = nextLast
	}
	return normalizeTags(tags), nil
}

func (s *Service) fetchTagPage(ctx context.Context, req ManifestRequest, last string) ([]string, string, error) {
	resp, err := s.client.ListTags(ctx, upstream.ListTagsRequest{
		UpstreamAlias: req.Alias,
		Repo:          req.Repository,
		N:             strconv.Itoa(s.opts.TagPageSize),
		Last:          last,
	})
	if err != nil {
		return nil, "", wrapError(err, "list tags for manifest suggestions")
	}

	body, err := readResponseBody(resp)
	if err != nil {
		return nil, "", err
	}
	page, err := decodeTagList(body)
	if err != nil {
		return nil, "", err
	}
	return page.Tags, nextLastFromLink(resp.Headers.Get(distribution.HeaderLink)), nil
}

const maxTagListBodyBytes int64 = 16 << 20

func readResponseBody(resp *upstream.TagsResponse) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, errorf("tags response body is required")
	}
	data, readErr := upstreamhttp.ReadAllLimited(resp.Body, maxTagListBodyBytes)
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		err := multierr.Combine(readErr, closeErr)
		return nil, wrapError(err, "read tags response body")
	}
	return data, nil
}

func decodeTagList(data []byte) (tagListBody, error) {
	var body tagListBody
	if err := json.Unmarshal(data, &body); err != nil {
		return tagListBody{}, wrapError(err, "decode tags response body")
	}
	return body, nil
}

func normalizeTags(tags []string) []string {
	return lo.Uniq(lo.FilterMap(tags, func(tag string, _ int) (string, bool) {
		tag = strings.TrimSpace(tag)
		return tag, tag != ""
	}))
}

func nextLastFromLink(header string) string {
	for part := range strings.SplitSeq(header, ",") {
		if last := nextLastFromLinkPart(part); last != "" {
			return last
		}
	}
	return ""
}

func nextLastFromLinkPart(part string) string {
	if !nextLinkPart(part) {
		return ""
	}
	target := linkTarget(part)
	if target == "" {
		return ""
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("last"))
}

func nextLinkPart(part string) bool {
	return strings.Contains(part, `rel="next"`) || strings.Contains(part, "rel=next")
}

func linkTarget(part string) string {
	left := strings.Index(part, "<")
	right := strings.Index(part, ">")
	if left < 0 || right <= left {
		return ""
	}
	return strings.TrimSpace(part[left+1 : right])
}

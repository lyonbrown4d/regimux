package pypi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

var errSimpleIndexOutputLimit = errors.New("rewritten pypi simple index exceeds output limit")

func RewriteSimpleIndexLinks(body []byte, alias, upstreamSimpleURL string) []byte {
	return rewriteSimpleIndexLinks(body, alias, upstreamSimpleURL, "")
}

func rewriteSimpleIndexLinks(body []byte, alias, upstreamSimpleURL, publicURL string) []byte {
	rewritten, err := rewriteSimpleIndexLinksLimited(body, alias, upstreamSimpleURL, publicURL, int64(^uint64(0)>>1))
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteSimpleIndexLinksLimited(
	body []byte,
	alias string,
	upstreamSimpleURL string,
	publicURL string,
	maxBytes int64,
) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	upstreamURL, err := url.Parse(upstreamSimpleURL)
	if err != nil {
		return nil, fmt.Errorf("parse upstream simple index URL: %w", err)
	}
	rewritten, changed, err := rewriteSimpleIndexHrefs(body, alias, upstreamURL, publicURL, maxBytes)
	if err != nil {
		return nil, err
	}
	if rewritten == nil || !changed {
		return body, nil
	}
	return rewritten, nil
}

func rewriteSimpleIndexHrefs(
	body []byte,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
	maxBytes int64,
) ([]byte, bool, error) {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var rewritten bytes.Buffer
	changed := false
	for {
		token, tokenChanged, done, err := rewriteNextSimpleIndexToken(tokenizer, alias, upstreamURL, publicURL, maxBytes-int64(rewritten.Len()))
		if err != nil {
			return nil, false, err
		}
		if done {
			return rewritten.Bytes(), changed, nil
		}
		changed = changed || tokenChanged
		if _, err := rewritten.Write(token); err != nil {
			return nil, false, fmt.Errorf("write rewritten pypi simple index: %w", err)
		}
	}
}

func rewriteNextSimpleIndexToken(
	tokenizer *html.Tokenizer,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
	remaining int64,
) ([]byte, bool, bool, error) {
	tokenType := tokenizer.Next()
	if tokenType == html.ErrorToken {
		if errors.Is(tokenizer.Err(), io.EOF) {
			return nil, false, true, nil
		}
		return nil, false, false, fmt.Errorf("tokenize pypi simple index: %w", tokenizer.Err())
	}
	token, changed, err := rewriteSimpleIndexToken(tokenType, tokenizer.Raw(), alias, upstreamURL, publicURL, remaining)
	if err != nil {
		return nil, false, false, err
	}
	if int64(len(token)) > remaining {
		return nil, false, false, errSimpleIndexOutputLimit
	}
	return token, changed, false, nil
}

func rewriteSimpleIndexToken(
	tokenType html.TokenType,
	raw []byte,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
	remaining int64,
) ([]byte, bool, error) {
	if !tokenCanHaveHref(tokenType) {
		return raw, false, nil
	}
	return rewriteTagHrefs(raw, alias, upstreamURL, publicURL, remaining)
}

func tokenCanHaveHref(tokenType html.TokenType) bool {
	return tokenType == html.StartTagToken || tokenType == html.SelfClosingTagToken
}

func rewriteTagHrefs(
	raw []byte,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
	remaining int64,
) ([]byte, bool, error) {
	tokenSize := int64(len(raw))
	replacements, err := tagHrefReplacements(raw, alias, upstreamURL, publicURL, remaining, &tokenSize)
	if err != nil {
		return nil, false, err
	}
	if len(replacements) == 0 {
		return raw, false, nil
	}
	out := make([]byte, 0, len(raw))
	last := 0
	for _, replacement := range replacements {
		out = append(out, raw[last:replacement.start]...)
		out = append(out, replacement.value...)
		last = replacement.end
	}
	out = append(out, raw[last:]...)
	return out, true, nil
}

func tagHrefReplacements(
	raw []byte,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
	remaining int64,
	tokenSize *int64,
) ([]hrefReplacement, error) {
	scanner := newTagAttrScanner(raw)
	replacements := make([]hrefReplacement, 0, 1)
	for {
		attr, ok := scanner.next()
		if !ok {
			break
		}
		replacement, replace, err := hrefReplacementForAttr(attr, alias, upstreamURL, publicURL, remaining, tokenSize)
		if err != nil {
			return nil, err
		}
		if replace {
			replacements = append(replacements, replacement)
		}
	}
	return replacements, nil
}

type hrefReplacement struct {
	start int
	end   int
	value string
}

func hrefReplacementForAttr(
	attr tagAttr,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
	remaining int64,
	tokenSize *int64,
) (hrefReplacement, bool, error) {
	if !attr.quotedValue || !isHrefAttrName(attr.name) {
		return hrefReplacement{}, false, nil
	}
	local := localPackageHref(alias, upstreamURL, string(attr.value))
	if local == "" {
		return hrefReplacement{}, false, nil
	}
	valueLength := int64(len(local))
	prefix := ""
	if publicURL != "" {
		prefix = strings.TrimRight(publicURL, "/")
		valueLength += int64(len(prefix))
	}
	delta := valueLength - int64(attr.valueEnd-attr.valueStart)
	if delta > 0 && *tokenSize+delta > remaining {
		return hrefReplacement{}, false, errSimpleIndexOutputLimit
	}
	if prefix != "" {
		local = prefix + local
	}
	*tokenSize += delta
	return hrefReplacement{start: attr.valueStart, end: attr.valueEnd, value: local}, true, nil
}

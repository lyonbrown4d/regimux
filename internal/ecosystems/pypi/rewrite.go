package pypi

import (
	"bytes"
	"errors"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func RewriteSimpleIndexLinks(body []byte, alias, upstreamSimpleURL string) []byte {
	return rewriteSimpleIndexLinks(body, alias, upstreamSimpleURL, "")
}

func rewriteSimpleIndexLinks(body []byte, alias, upstreamSimpleURL, publicURL string) []byte {
	if len(body) == 0 {
		return body
	}
	upstreamURL, err := url.Parse(upstreamSimpleURL)
	if err != nil {
		return body
	}
	rewritten, changed, ok := rewriteSimpleIndexHrefs(body, alias, upstreamURL, publicURL)
	if !ok || !changed {
		return body
	}
	return rewritten
}

func rewriteSimpleIndexHrefs(body []byte, alias string, upstreamURL *url.URL, publicURL string) ([]byte, bool, bool) {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var rewritten bytes.Buffer
	changed := false
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			return rewritten.Bytes(), changed, errors.Is(tokenizer.Err(), io.EOF)
		}
		token, tokenChanged := rewriteSimpleIndexToken(tokenType, tokenizer.Raw(), alias, upstreamURL, publicURL)
		changed = changed || tokenChanged
		if !writeToken(&rewritten, token) {
			return nil, false, false
		}
	}
}

func rewriteSimpleIndexToken(
	tokenType html.TokenType,
	raw []byte,
	alias string,
	upstreamURL *url.URL,
	publicURL string,
) ([]byte, bool) {
	if !tokenCanHaveHref(tokenType) {
		return raw, false
	}
	return rewriteTagHrefs(raw, alias, upstreamURL, publicURL)
}

func tokenCanHaveHref(tokenType html.TokenType) bool {
	return tokenType == html.StartTagToken || tokenType == html.SelfClosingTagToken
}

func writeToken(buf *bytes.Buffer, raw []byte) bool {
	_, err := buf.Write(raw)
	return err == nil
}

func localPackageHref(alias string, upstreamSimpleURL *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/pypi/") {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := upstreamSimpleURL.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	path := strings.TrimLeft(resolved.EscapedPath(), "/")
	if path == "" {
		return ""
	}
	local := "/pypi/" + url.PathEscape(alias) + "/packages/" + resolved.Scheme + "/" + resolved.Host + "/" + path
	if resolved.RawQuery != "" {
		local += "?" + resolved.RawQuery
	}
	if parsed.Fragment != "" {
		local += "#" + parsed.Fragment
	}
	return local
}

type hrefReplacement struct {
	start int
	end   int
	value string
}

func rewriteTagHrefs(raw []byte, alias string, upstreamURL *url.URL, publicURL string) ([]byte, bool) {
	replacements := tagHrefReplacements(raw, alias, upstreamURL, publicURL)
	if len(replacements) == 0 {
		return raw, false
	}
	out := make([]byte, 0, len(raw))
	last := 0
	for _, replacement := range replacements {
		out = append(out, raw[last:replacement.start]...)
		out = append(out, replacement.value...)
		last = replacement.end
	}
	out = append(out, raw[last:]...)
	return out, true
}

func tagHrefReplacements(raw []byte, alias string, upstreamURL *url.URL, publicURL string) []hrefReplacement {
	scanner := newTagAttrScanner(raw)
	replacements := make([]hrefReplacement, 0, 1)
	for {
		attr, ok := scanner.next()
		if !ok {
			break
		}
		if replacement, replace := hrefReplacementForAttr(attr, alias, upstreamURL, publicURL); replace {
			replacements = append(replacements, replacement)
		}
	}
	return replacements
}

type tagAttr struct {
	name        []byte
	value       []byte
	valueStart  int
	valueEnd    int
	quotedValue bool
}

type tagAttrScanner struct {
	raw []byte
	pos int
}

func newTagAttrScanner(raw []byte) tagAttrScanner {
	return tagAttrScanner{raw: raw, pos: skipTagName(raw)}
}

func (s *tagAttrScanner) next() (tagAttr, bool) {
	for {
		s.pos = skipHTMLSpaces(s.raw, s.pos)
		if s.done() {
			return tagAttr{}, false
		}
		if s.raw[s.pos] != '/' {
			return s.scan()
		}
		s.pos++
	}
}

func (s *tagAttrScanner) done() bool {
	return s.pos >= len(s.raw) || s.raw[s.pos] == '>'
}

func (s *tagAttrScanner) scan() (tagAttr, bool) {
	name := s.scanName()
	if len(name) == 0 {
		s.pos++
		return tagAttr{}, true
	}
	if !s.consumeEqual() {
		return tagAttr{name: name}, true
	}
	return s.scanValue(name), true
}

func (s *tagAttrScanner) scanName() []byte {
	nameStart := s.pos
	for s.pos < len(s.raw) && isAttrNameByte(s.raw[s.pos]) {
		s.pos++
	}
	return s.raw[nameStart:s.pos]
}

func (s *tagAttrScanner) consumeEqual() bool {
	s.pos = skipHTMLSpaces(s.raw, s.pos)
	if s.pos >= len(s.raw) || s.raw[s.pos] != '=' {
		return false
	}
	s.pos++
	s.pos = skipHTMLSpaces(s.raw, s.pos)
	return s.pos < len(s.raw)
}

func (s *tagAttrScanner) scanValue(name []byte) tagAttr {
	quote := s.raw[s.pos]
	if quote != '"' && quote != '\'' {
		return s.scanUnquotedValue(name)
	}
	start := s.pos + 1
	end := scanQuotedValueEnd(s.raw, start, quote)
	s.pos = min(end+1, len(s.raw))
	quoted := end < len(s.raw)
	return tagAttr{name: name, value: s.raw[start:end], valueStart: start, valueEnd: end, quotedValue: quoted}
}

func (s *tagAttrScanner) scanUnquotedValue(name []byte) tagAttr {
	start := s.pos
	for s.pos < len(s.raw) && !isHTMLSpace(s.raw[s.pos]) && s.raw[s.pos] != '>' {
		s.pos++
	}
	return tagAttr{name: name, value: s.raw[start:s.pos], valueStart: start, valueEnd: s.pos}
}

func scanQuotedValueEnd(raw []byte, start int, quote byte) int {
	end := start
	for end < len(raw) && raw[end] != quote {
		end++
	}
	return end
}

func hrefReplacementForAttr(attr tagAttr, alias string, upstreamURL *url.URL, publicURL string) (hrefReplacement, bool) {
	if !attr.quotedValue || !isHrefAttrName(attr.name) {
		return hrefReplacement{}, false
	}
	local := localPackageHref(alias, upstreamURL, string(attr.value))
	if local == "" {
		return hrefReplacement{}, false
	}
	if publicURL != "" {
		local = strings.TrimRight(publicURL, "/") + local
	}
	return hrefReplacement{start: attr.valueStart, end: attr.valueEnd, value: local}, true
}

func skipTagName(raw []byte) int {
	i := 0
	if i < len(raw) && raw[i] == '<' {
		i++
	}
	for i < len(raw) && !isHTMLSpace(raw[i]) && raw[i] != '/' && raw[i] != '>' {
		i++
	}
	return i
}

func skipHTMLSpaces(raw []byte, i int) int {
	for i < len(raw) && isHTMLSpace(raw[i]) {
		i++
	}
	return i
}

func isHTMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

func isAttrNameByte(b byte) bool {
	return !isHTMLSpace(b) && b != '=' && b != '/' && b != '>'
}

func isHrefAttrName(name []byte) bool {
	return len(name) == 4 &&
		lowerASCII(name[0]) == 'h' &&
		lowerASCII(name[1]) == 'r' &&
		lowerASCII(name[2]) == 'e' &&
		lowerASCII(name[3]) == 'f'
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

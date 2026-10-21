package pypi

import (
	"net/url"
	"strings"
)

func localPackageHref(alias string, upstreamSimpleURL *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	localPackagePrefix := "/pypi/" + url.PathEscape(alias) + "/packages/"
	if strings.HasPrefix(parsed.Path, localPackagePrefix) {
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
	return addPackageIdentity(local, upstreamSimpleURL, parsed.Fragment)
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

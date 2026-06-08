package admin

import (
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (s *Service) syncUpstream(ecosystemName, alias string) (ecosystem.Upstream, bool) {
	ecosystemName = strings.TrimSpace(ecosystemName)
	alias = strings.TrimSpace(alias)
	if ecosystemName == "" || alias == "" {
		return ecosystem.Upstream{}, false
	}
	var match ecosystem.Upstream
	var ok bool
	s.configuredUpstreams().Range(func(_ int, upstream ecosystem.Upstream) bool {
		if upstream.Ecosystem != ecosystemName || upstream.Alias != alias {
			return true
		}
		match = upstream
		ok = true
		return false
	})
	return match, ok
}

func defaultSyncForm() SyncForm {
	return SyncForm{
		Reference: "latest",
	}
}

func (s *Service) defaultSyncUpstreamValue() string {
	var value string
	s.configuredUpstreams().Range(func(_ int, upstream ecosystem.Upstream) bool {
		value = syncTargetValue(upstream.Ecosystem, upstream.Alias)
		return false
	})
	return value
}

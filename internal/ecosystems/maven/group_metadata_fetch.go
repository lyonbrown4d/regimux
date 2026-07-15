package maven

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
)

const maxMavenMetadataBytes = 16 << 20

type mergedMetadataState struct {
	responses groupFetchState
	documents []mavenMetadataDocument
	sources   []string
	headers   http.Header
}

type metadataMemberResult struct {
	terminal *upstreamFetch
}

func (s *Service) fetchMergedGroupMetadata(
	ctx context.Context,
	group config.MavenGroupConfig,
	route Route,
	method string,
) (*upstreamFetch, error) {
	state := newMergedMetadataState(group)
	memberMethod := metadataRequestMethod(method)
	for _, member := range group.Members {
		result, err := s.collectGroupMetadataMember(
			ctx,
			group,
			route,
			member,
			memberMethod,
			state,
		)
		if err != nil {
			state.responses.close(s)
			return nil, err
		}
		if result.terminal != nil {
			state.responses.close(s)
			return result.terminal, nil
		}
	}
	return s.finishMergedGroupMetadata(state, route.Alias, method)
}

func newMergedMetadataState(group config.MavenGroupConfig) *mergedMetadataState {
	return &mergedMetadataState{
		documents: make([]mavenMetadataDocument, 0, len(group.Members)),
		sources:   make([]string, 0, len(group.Members)),
	}
}

func metadataRequestMethod(method string) string {
	if methodOrGet(method) == http.MethodHead {
		return http.MethodGet
	}
	return method
}

func (s *Service) collectGroupMetadataMember(
	ctx context.Context,
	group config.MavenGroupConfig,
	route Route,
	member, method string,
	state *mergedMetadataState,
) (metadataMemberResult, error) {
	fetched, err := s.fetchGroupMember(ctx, member, route, method)
	if err != nil {
		return metadataMemberResult{}, state.responses.rememberError(err, group.FallbackOnError)
	}

	switch classifyGroupResponse(fetched.status, group.FallbackOnError) {
	case groupResponseMiss:
		state.responses.rememberMiss(s, fetched)
		return metadataMemberResult{}, nil
	case groupResponseFallback:
		state.responses.rememberFailure(s, fetched)
		return metadataMemberResult{}, nil
	case groupResponseTerminal:
	}
	if fetched.status < http.StatusOK || fetched.status >= http.StatusMultipleChoices {
		return metadataMemberResult{terminal: fetched}, nil
	}

	document, err := s.consumeMetadata(fetched)
	if err != nil {
		return metadataMemberResult{}, state.responses.rememberError(err, group.FallbackOnError)
	}
	state.addDocument(member, fetched.headers, document)
	return metadataMemberResult{}, nil
}

func (state *mergedMetadataState) addDocument(
	member string,
	headers http.Header,
	document mavenMetadataDocument,
) {
	if state.headers == nil {
		state.headers = headers.Clone()
	}
	state.documents = append(state.documents, document)
	state.sources = append(state.sources, member)
}

func (s *Service) finishMergedGroupMetadata(
	state *mergedMetadataState,
	alias, method string,
) (*upstreamFetch, error) {
	if len(state.documents) == 0 {
		return state.responses.result(s, alias)
	}
	state.responses.close(s)

	payload, err := mergeMavenMetadata(state.documents)
	if err != nil {
		return nil, err
	}
	return newMergedMetadataFetch(payload, state.headers, state.sources, method), nil
}

func newMergedMetadataFetch(
	payload []byte,
	headers http.Header,
	sources []string,
	method string,
) *upstreamFetch {
	headers.Del("Content-Encoding")
	headers.Del("ETag")
	headers.Del("Last-Modified")
	headers.Set("Content-Type", "application/xml")
	headers.Set("Content-Length", strconv.Itoa(len(payload)))
	headers.Set(resolvedUpstreamHeader, strings.Join(sources, ","))

	body := io.NopCloser(bytes.NewReader(payload))
	if methodOrGet(method) == http.MethodHead {
		body = http.NoBody
	}
	return &upstreamFetch{
		status:  http.StatusOK,
		headers: headers,
		body:    body,
	}
}

func (s *Service) consumeMetadata(fetched *upstreamFetch) (mavenMetadataDocument, error) {
	data, err := io.ReadAll(io.LimitReader(fetched.body, maxMavenMetadataBytes+1))
	closeReadCloser(fetched.body, s.logger, "close Maven group metadata")
	if err != nil {
		return mavenMetadataDocument{}, fmt.Errorf("read Maven group metadata: %w", err)
	}
	if len(data) > maxMavenMetadataBytes {
		return mavenMetadataDocument{}, fmt.Errorf(
			"read Maven group metadata: document exceeds %d bytes",
			maxMavenMetadataBytes,
		)
	}
	document, err := parseMavenMetadata(data)
	if err != nil {
		return mavenMetadataDocument{}, fmt.Errorf("parse Maven group metadata: %w", err)
	}
	return document, nil
}

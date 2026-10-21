package maven

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven/metadataxml"
	"github.com/lyonbrown4d/regimux/internal/spool"
)

const maxMavenMetadataBytes int64 = 16 << 20

var errMavenGroupMetadataLimit = errors.New("maven group metadata exceeds size limit")

type mergedMetadataState struct {
	responses  groupFetchState
	documents  []mavenMetadataDocument
	sources    []string
	headers    http.Header
	inputBytes int64
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
	if err := s.metadataGate.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("admit Maven group metadata: %w", err)
	}
	defer s.metadataGate.Release(1)

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
	return s.finishMergedGroupMetadata(ctx, state, route.Alias, method)
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

	remaining := maxMavenMetadataBytes - state.inputBytes
	document, size, err := s.consumeMetadata(fetched, remaining)
	state.inputBytes += size
	if errors.Is(err, errMavenGroupMetadataLimit) {
		return metadataMemberResult{}, err
	}
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
	ctx context.Context,
	state *mergedMetadataState,
	alias, method string,
) (*upstreamFetch, error) {
	if len(state.documents) == 0 {
		return state.responses.result(s, alias)
	}
	state.responses.close(s)

	document, err := buildMergedMavenMetadata(state.documents)
	if err != nil {
		return nil, err
	}
	return newMergedMetadataFetch(ctx, document, state.headers, state.sources, method)
}

func (s *Service) consumeMetadata(
	fetched *upstreamFetch,
	limit int64,
) (mavenMetadataDocument, int64, error) {
	data, err := io.ReadAll(io.LimitReader(fetched.body, limit+1))
	closeReadCloser(fetched.body, s.logger, "close Maven group metadata")
	if err != nil {
		return mavenMetadataDocument{}, int64(len(data)), fmt.Errorf("read Maven group metadata: %w", err)
	}
	if int64(len(data)) > limit {
		return mavenMetadataDocument{}, int64(len(data)), fmt.Errorf(
			"read Maven group metadata: group metadata input exceeds %d bytes: %w",
			maxMavenMetadataBytes,
			errMavenGroupMetadataLimit,
		)
	}
	document, err := parseMavenMetadata(data)
	if err != nil {
		return mavenMetadataDocument{}, int64(len(data)), fmt.Errorf("parse Maven group metadata: %w", err)
	}
	return document, int64(len(data)), nil
}

func newMergedMetadataFetch(
	ctx context.Context,
	document mavenMetadataDocument,
	headers http.Header,
	sources []string,
	method string,
) (*upstreamFetch, error) {
	headers = headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Del("Content-Encoding")
	headers.Del("ETag")
	headers.Del("Last-Modified")
	headers.Set("Content-Type", "application/xml")
	headers.Set(resolvedUpstreamHeader, strings.Join(sources, ","))

	var body io.ReadCloser = http.NoBody
	var size int64
	var err error
	if methodOrGet(method) != http.MethodHead {
		body, size, err = spoolMergedMetadata(ctx, document)
		if err != nil {
			return nil, err
		}
	} else {
		size, err = metadataxml.Encode(ctx, io.Discard, document, maxMavenMetadataBytes)
		if err != nil {
			return nil, fmt.Errorf("serialize Maven group metadata: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		if body != http.NoBody {
			return nil, errors.Join(fmt.Errorf("serialize Maven group metadata: %w", err), body.Close())
		}
		return nil, fmt.Errorf("serialize Maven group metadata: %w", err)
	}
	headers.Set("Content-Length", strconv.FormatInt(size, 10))
	return &upstreamFetch{
		status:  http.StatusOK,
		headers: headers,
		body:    body,
	}, nil
}

func spoolMergedMetadata(ctx context.Context, document mavenMetadataDocument) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, fmt.Errorf("spool Maven group metadata: %w", err)
	}
	tmp, err := spool.NewTempBody("regimux-maven-group-metadata-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create Maven group metadata spool: %w", err)
	}
	size, err := metadataxml.Encode(ctx, tmp, document, maxMavenMetadataBytes)
	if err != nil {
		return nil, 0, errors.Join(fmt.Errorf("serialize Maven group metadata: %w", err), tmp.Close())
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, errors.Join(fmt.Errorf("spool Maven group metadata: %w", err), tmp.Close())
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, 0, errors.Join(fmt.Errorf("rewind Maven group metadata spool: %w", err), tmp.Close())
	}
	return tmp, size, nil
}

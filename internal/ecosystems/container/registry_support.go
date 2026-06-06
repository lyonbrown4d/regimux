package container

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/arcgolabs/httpx"
	"github.com/danielgtaylor/huma/v2"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func (i registryInput) path() string {
	tail := strings.TrimPrefix(i.Tail.String(), "/")
	if tail == "" {
		return "/v2/" + i.Alias
	}
	return "/v2/" + i.Alias + "/" + tail
}

type registryOutput struct {
	Status                       int
	ContentType                  string `header:"Content-Type"`
	ContentLength                string `header:"Content-Length"`
	DockerDistributionAPIVersion string `header:"Docker-Distribution-Api-Version"`
	DockerContentDigest          string `header:"Docker-Content-Digest"`
	AcceptRanges                 string `header:"Accept-Ranges"`
	ContentRange                 string `header:"Content-Range"`
	ETag                         string `header:"ETag"`
	Link                         string `header:"Link"`
	Location                     string `header:"Location"`
	Warning                      string `header:"Warning"`
	XMirrorCache                 string `header:"X-Mirror-Cache"`
	Body                         httpx.ResponseStream
}

func newRegistryOutput(status int, header http.Header) *registryOutput {
	out := &registryOutput{
		Status:                       status,
		DockerDistributionAPIVersion: distribution.APIVersion,
		Body:                         streamWithStatus(status, nil),
	}
	if header == nil {
		return out
	}
	out.ContentLength = header.Get(distribution.HeaderContentLength)
	out.ContentRange = header.Get(distribution.HeaderContentRange)
	out.ETag = header.Get(distribution.HeaderETag)
	out.Link = header.Get(distribution.HeaderLink)
	out.Location = header.Get(distribution.HeaderLocation)
	out.Warning = header.Get(distribution.HeaderWarning)
	return out
}

func routeFromInput(input *registryInput) (reference.Route, error) {
	if input == nil {
		return reference.Route{}, distribution.ErrNameInvalid.WithDetail("registry input is nil")
	}
	route, err := reference.Parse(input.path())
	if err != nil {
		return reference.Route{}, oops.Wrapf(err, "parse registry route")
	}
	return route, nil
}

func defaultNamespacesFromConfig(cfg config.Config) *collectionmapping.Map[string, string] {
	upstreams := cfg.OrderedContainerUpstreams()
	out := collectionmapping.NewMapWithCapacity[string, string](upstreams.Len())
	upstreams.Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		namespace := strings.Trim(strings.TrimSpace(upstreamCfg.DefaultNamespace), "/")
		if strings.TrimSpace(alias) == "" || namespace == "" {
			return true
		}
		out.Set(alias, namespace)
		return true
	})
	return out
}

func errorOutput(err error) *registryOutput {
	list := distribution.FromError(err)
	if list == nil {
		list = distribution.ErrUnknown.WithDetail(nil)
	}
	status := list.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	body, marshalErr := distribution.MarshalError(list)
	if marshalErr != nil {
		body = []byte(`{"errors":[{"code":"UNKNOWN","message":"unknown error"}]}`)
	}
	return &registryOutput{
		Status:                       status,
		ContentType:                  distribution.MediaTypeJSON,
		ContentLength:                strconv.Itoa(len(body)),
		DockerDistributionAPIVersion: distribution.APIVersion,
		Body:                         streamWithStatus(status, httpx.StreamReader(bytes.NewReader(body))),
	}
}

func streamWithStatus(status int, stream httpx.ResponseStream) httpx.ResponseStream {
	return func(ctx huma.Context) {
		if ctx == nil {
			return
		}
		ctx.SetStatus(status)
		if stream != nil {
			stream(ctx)
		}
	}
}

func unsupported(method, path string) *distribution.ErrorList {
	return distribution.Unsupported(method, path)
}

func endpointSpec(tags ...string) httpx.EndpointSpec {
	return httpx.EndpointSpec{
		Tags:       httpx.Tags(tags...),
		Security:   httpx.SecurityRequirements(),
		Parameters: httpx.Parameters(),
		Extensions: httpx.Extensions(nil),
	}
}

func registryOperationDocs() []httpx.OperationOption {
	return []httpx.OperationOption{
		httpx.OperationBinaryResponse(
			distribution.MediaTypeOctetStream,
			distribution.MediaTypeJSON,
			distribution.MediaTypeDockerManifest,
			distribution.MediaTypeDockerManifestList,
			distribution.MediaTypeOCIManifest,
			distribution.MediaTypeOCIIndex,
		),
	}
}

func operationID(id string) httpx.OperationOption {
	return func(op *huma.Operation) {
		if op != nil {
			op.OperationID = id
		}
	}
}

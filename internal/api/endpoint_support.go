// Package api wires the shared HTTP server and common endpoints.
package api

import "github.com/arcgolabs/httpx"

func endpointSpec(tags ...string) httpx.EndpointSpec {
	return httpx.EndpointSpec{
		Tags:       httpx.Tags(tags...),
		Security:   httpx.SecurityRequirements(),
		Parameters: httpx.Parameters(),
		Extensions: httpx.Extensions(nil),
	}
}

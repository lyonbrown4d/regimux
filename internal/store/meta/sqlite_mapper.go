package meta

import (
	mapperx "github.com/arcgolabs/mapper"
)

var metadataRowMapper = mapperx.New(
	mapperx.Converter(unixNano),
	mapperx.Converter(timeFromUnixNano),
	mapperx.ConverterE(encodeHeaders),
	mapperx.ConverterE(decodeHeaders),
)

func mapMetadata[D any](src any) (D, error) {
	var dst D
	if err := metadataRowMapper.MapInto(&dst, src); err != nil {
		return dst, wrapError(err, "map metadata row")
	}
	return dst, nil
}

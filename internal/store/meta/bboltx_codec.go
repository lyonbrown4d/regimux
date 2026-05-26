package meta

import "github.com/arcgolabs/storx/keycodec"

func manifestKeyCodec() keycodec.Codec[ManifestKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key ManifestKey) string { return key.Alias },
			func(target *ManifestKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key ManifestKey) string { return key.Repository },
			func(target *ManifestKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key ManifestKey) string { return key.Digest },
			func(target *ManifestKey, value string) { target.Digest = value },
		),
	)
}

func tagKeyCodec() keycodec.Codec[TagKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key TagKey) string { return key.Alias },
			func(target *TagKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key TagKey) string { return key.Repository },
			func(target *TagKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key TagKey) string { return key.Reference },
			func(target *TagKey, value string) { target.Reference = value },
		),
	)
}

func pullKeyCodec() keycodec.Codec[PullKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key PullKey) string { return key.Alias },
			func(target *PullKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key PullKey) string { return key.Repository },
			func(target *PullKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key PullKey) string { return key.Reference },
			func(target *PullKey, value string) { target.Reference = value },
		),
	)
}

func blobKeyCodec() keycodec.Codec[BlobKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key BlobKey) string { return key.Digest },
			func(target *BlobKey, value string) { target.Digest = value },
		),
	)
}

func repoBlobKeyCodec() keycodec.Codec[RepoBlobKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key RepoBlobKey) string { return key.Alias },
			func(target *RepoBlobKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key RepoBlobKey) string { return key.Repository },
			func(target *RepoBlobKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key RepoBlobKey) string { return key.Digest },
			func(target *RepoBlobKey, value string) { target.Digest = value },
		),
	)
}

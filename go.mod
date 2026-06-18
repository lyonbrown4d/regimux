module github.com/lyonbrown4d/regimux

go 1.26.3

require (
	github.com/Masterminds/semver/v3 v3.5.0
	github.com/agext/levenshtein v1.2.3
	github.com/arcgolabs/authx v0.3.2
	github.com/arcgolabs/authx/http/fiber v0.4.0
	github.com/arcgolabs/authx/jwt v0.3.0
	github.com/arcgolabs/clientx v0.1.3
	github.com/arcgolabs/collectionx/bitset v0.9.0
	github.com/arcgolabs/collectionx/interval v0.9.0
	github.com/arcgolabs/collectionx/list v0.9.0
	github.com/arcgolabs/collectionx/mapping v0.9.0
	github.com/arcgolabs/collectionx/set v0.9.0
	github.com/arcgolabs/configx v0.6.1
	github.com/arcgolabs/configx/format/hcl v0.6.1
	github.com/arcgolabs/dbx v0.1.12
	github.com/arcgolabs/dbx/migrate v0.1.6
	github.com/arcgolabs/dix v0.11.1
	github.com/arcgolabs/eventx v0.1.2
	github.com/arcgolabs/httpx v0.1.8
	github.com/arcgolabs/httpx/adapter/fiber v0.1.8
	github.com/arcgolabs/kvx v0.3.1
	github.com/arcgolabs/kvx/adapter/redis v0.2.2
	github.com/arcgolabs/kvx/adapter/valkey v0.2.2
	github.com/arcgolabs/logx v0.1.3
	github.com/arcgolabs/mapper v0.2.0
	github.com/arcgolabs/observabilityx v0.4.0
	github.com/aws/aws-sdk-go-v2 v1.42.0
	github.com/aws/aws-sdk-go-v2/config v1.32.25
	github.com/aws/aws-sdk-go-v2/credentials v1.19.24
	github.com/aws/aws-sdk-go-v2/service/s3 v1.104.0
	github.com/containerd/platforms v0.2.1
	github.com/danielgtaylor/huma/v2 v2.38.0
	github.com/distribution/reference v0.6.0
	github.com/dustin/go-humanize v1.0.1
	github.com/fclairamb/afero-s3 v0.4.0
	github.com/go-co-op/gocron-redis-lock/v2 v2.2.1
	github.com/go-co-op/gocron/v2 v2.21.2
	github.com/go-playground/validator/v10 v10.30.3
	github.com/go-sql-driver/mysql v1.10.0
	github.com/gofiber/fiber/v3 v3.3.0
	github.com/gofiber/template/html/v3 v3.0.5
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/lib/pq v1.12.3
	github.com/moby/moby/api v1.54.2
	github.com/moby/moby/client v0.4.1
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/panjf2000/ants/v2 v2.12.1
	github.com/pkg/sftp v1.13.10
	github.com/redis/go-redis/v9 v9.20.1
	github.com/samber/go-singleflightx v0.3.2
	github.com/samber/lo v1.53.0
	github.com/samber/mo v1.17.0
	github.com/samber/oops v1.22.0
	github.com/samber/slog-fiber v1.22.2
	github.com/sethvargo/go-retry v0.3.0
	github.com/sourcegraph/conc v0.3.0
	github.com/spf13/afero v1.15.0
	github.com/spf13/afero/sftpfs v1.15.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	github.com/valkey-io/valkey-go v1.0.75
	go.uber.org/multierr v1.11.0
	golang.org/x/crypto v0.53.0
	golang.org/x/net v0.56.0
	golang.org/x/sync v0.21.0
	modernc.org/sqlite v1.52.0
	oras.land/oras-go/v2 v2.6.1
	resty.dev/v3 v3.0.0-rc.2
)

require (
	ariga.io/atlas v1.2.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/DmitriyVTitov/size v1.5.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/arcgolabs/collectionx/graph v0.9.0 // indirect
	github.com/arcgolabs/httpx/adapter/std v0.1.8 // indirect
	github.com/arcgolabs/pkg/option v0.0.3 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.13 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.29 // indirect
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.22.28 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.22 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.3 // indirect
	github.com/aws/smithy-go v1.27.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bmatcuk/doublestar v1.3.4 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/docker v28.5.2+incompatible // indirect
	github.com/docker/go-connections v0.7.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/go-chi/chi/v5 v5.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/inflect v0.21.6 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-redsync/redsync/v4 v4.16.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gofiber/fiber/v2 v2.52.13 // indirect
	github.com/gofiber/schema v1.8.0 // indirect
	github.com/gofiber/template/v2 v2.1.0 // indirect
	github.com/gofiber/utils/v2 v2.1.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/hashicorp/hcl/v2 v2.24.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/knadh/koanf/parsers/hcl v1.0.0 // indirect
	github.com/knadh/koanf/providers/confmap v1.0.0 // indirect
	github.com/knadh/koanf/providers/env/v2 v2.0.0 // indirect
	github.com/knadh/koanf/providers/file v1.2.1 // indirect
	github.com/knadh/koanf/v2 v2.3.5 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/pressly/goose/v3 v3.27.1 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.68.1 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/rs/zerolog v1.35.1 // indirect
	github.com/samber/do/v2 v2.0.0 // indirect
	github.com/samber/go-type-to-string v1.8.0 // indirect
	github.com/samber/hot v0.13.0 // indirect
	github.com/samber/slog-common v0.22.0 // indirect
	github.com/samber/slog-zerolog/v2 v2.9.2 // indirect
	github.com/segmentio/ksuid v1.0.4 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stephenafamo/scan v0.7.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.71.0 // indirect
	github.com/zclconf/go-cty v1.18.1 // indirect
	github.com/zclconf/go-cty-yaml v1.2.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/tools v0.46.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

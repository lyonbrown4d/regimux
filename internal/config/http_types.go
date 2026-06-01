package config

import "time"

type HTTPConfig struct {
	Timeout time.Duration   `json:"timeout" koanf:"timeout" mapstructure:"timeout" validate:"min=0"`
	HTTP2   HTTP2Config     `json:"http2"   koanf:"http2"   mapstructure:"http2"`
	Retry   HTTPRetryConfig `json:"retry"   koanf:"retry"   mapstructure:"retry"`
	TLS     HTTPTLSConfig   `json:"tls"     koanf:"tls"     mapstructure:"tls"`
}

type HTTP2Config struct {
	Enabled bool `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
}

type HTTPRetryConfig struct {
	Enabled    bool          `json:"enabled"     koanf:"enabled"     mapstructure:"enabled"`
	MaxRetries int           `json:"max_retries" koanf:"max_retries" mapstructure:"max_retries" validate:"min=0"`
	WaitMin    time.Duration `json:"wait_min"    koanf:"wait_min"    mapstructure:"wait_min"    validate:"min=0"`
	WaitMax    time.Duration `json:"wait_max"    koanf:"wait_max"    mapstructure:"wait_max"    validate:"min=0"`
}

type HTTPTLSConfig struct {
	Enabled            bool   `json:"enabled"              koanf:"enabled"              mapstructure:"enabled"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify" koanf:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
	ServerName         string `json:"server_name"          koanf:"server_name"          mapstructure:"server_name"`
}

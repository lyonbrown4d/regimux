package config

import "time"

type DockerConfig struct {
	Enabled bool                `json:"enabled" koanf:"enabled" mapstructure:"enabled"`
	Host    string              `json:"host"    koanf:"host"    mapstructure:"host"`
	Observe bool                `json:"observe" koanf:"observe" mapstructure:"observe"`
	Prewarm DockerPrewarmConfig `json:"prewarm" koanf:"prewarm" mapstructure:"prewarm"`
}

type DockerPrewarmConfig struct {
	Enabled  bool          `json:"enabled"  koanf:"enabled"  mapstructure:"enabled"`
	Registry string        `json:"registry" koanf:"registry" mapstructure:"registry"`
	Alias    string        `json:"alias"    koanf:"alias"    mapstructure:"alias"`
	Images   []string      `json:"images"   koanf:"images"   mapstructure:"images"   validate:"dive,required"`
	Timeout  time.Duration `json:"timeout"  koanf:"timeout"  mapstructure:"timeout"  validate:"min=0"`
	Platform string        `json:"platform" koanf:"platform" mapstructure:"platform"`
}

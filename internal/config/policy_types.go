package config

type PolicyConfig struct {
	Dependency DependencyPolicyConfig `json:"dependency" koanf:"dependency" mapstructure:"dependency"`
}

type DependencyPolicyConfig struct {
	Allow []DependencyRuleConfig `json:"allow" koanf:"allow" mapstructure:"allow"`
	Block []DependencyRuleConfig `json:"block" koanf:"block" mapstructure:"block"`
}

type DependencyRuleConfig struct {
	Ecosystem string `json:"ecosystem" koanf:"ecosystem" mapstructure:"ecosystem"`
	Alias     string `json:"alias"     koanf:"alias"     mapstructure:"alias"`
	Artifact  string `json:"artifact"  koanf:"artifact"  mapstructure:"artifact"`
	Reference string `json:"reference" koanf:"reference" mapstructure:"reference"`
}

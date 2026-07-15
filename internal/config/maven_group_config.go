package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	MavenMetadataPolicyMerge = "merge"
	MavenMetadataPolicyFirst = "first"
)

// MavenGroupConfig aggregates independent Maven repositories behind one logical alias.
type MavenGroupConfig struct {
	Members         []string `json:"members"           koanf:"members"           mapstructure:"members"`
	FallbackOnError bool     `json:"fallback_on_error" koanf:"fallback_on_error" mapstructure:"fallback_on_error"`
	MetadataPolicy  string   `json:"metadata_policy"   koanf:"metadata_policy"   mapstructure:"metadata_policy"`
}

// MavenGroupsConfig maps logical Maven group aliases to ordered physical repository members.
type MavenGroupsConfig map[string]MavenGroupConfig

// MavenGroup resolves one logical Maven group alias.
func (c Config) MavenGroup(alias string) (MavenGroupConfig, bool) {
	group, ok := c.MavenGroups[strings.TrimSpace(alias)]
	return group, ok
}

// OrderedMavenGroups returns Maven group aliases in deterministic order.
func (c Config) OrderedMavenGroups() []string {
	aliases := make([]string, 0, len(c.MavenGroups))
	for alias := range c.MavenGroups {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func (c *Config) normalizeMavenGroups() error {
	groups, err := normalizeMavenGroupAliases(c.MavenGroups)
	if err != nil {
		return err
	}
	c.MavenGroups = groups

	for _, alias := range c.OrderedMavenGroups() {
		group, normalizeErr := c.normalizeMavenGroup(alias, c.MavenGroups[alias])
		if normalizeErr != nil {
			return normalizeErr
		}
		c.MavenGroups[alias] = group
	}
	return nil
}

func normalizeMavenGroupAliases(groups MavenGroupsConfig) (MavenGroupsConfig, error) {
	normalized := make(MavenGroupsConfig, len(groups))
	for rawAlias, group := range groups {
		alias := strings.TrimSpace(rawAlias)
		if alias == "" {
			return nil, errors.New("normalize maven groups: alias must not be empty")
		}
		if _, exists := normalized[alias]; exists {
			return nil, fmt.Errorf("normalize maven groups: duplicate alias %q", alias)
		}
		normalized[alias] = group
	}
	return normalized, nil
}

func (c *Config) normalizeMavenGroup(
	alias string,
	group MavenGroupConfig,
) (MavenGroupConfig, error) {
	if _, exists := c.Maven[alias]; exists {
		return MavenGroupConfig{}, fmt.Errorf(
			"normalize maven group %q: alias conflicts with a Maven upstream",
			alias,
		)
	}

	policy, err := normalizeMavenMetadataPolicy(alias, group.MetadataPolicy)
	if err != nil {
		return MavenGroupConfig{}, err
	}
	members, err := c.normalizeMavenGroupMembers(alias, group.Members)
	if err != nil {
		return MavenGroupConfig{}, err
	}
	group.MetadataPolicy = policy
	group.Members = members
	return group, nil
}

func normalizeMavenMetadataPolicy(alias, policy string) (string, error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		return MavenMetadataPolicyMerge, nil
	}
	if policy != MavenMetadataPolicyMerge && policy != MavenMetadataPolicyFirst {
		return "", fmt.Errorf(
			"normalize maven group %q: metadata_policy must be %q or %q",
			alias,
			MavenMetadataPolicyMerge,
			MavenMetadataPolicyFirst,
		)
	}
	return policy, nil
}

func (c *Config) normalizeMavenGroupMembers(
	alias string,
	rawMembers []string,
) ([]string, error) {
	if len(rawMembers) == 0 {
		return nil, fmt.Errorf("normalize maven group %q: members must not be empty", alias)
	}

	members := make([]string, len(rawMembers))
	seen := make(map[string]struct{}, len(rawMembers))
	for index, rawMember := range rawMembers {
		member := strings.TrimSpace(rawMember)
		if err := c.validateMavenGroupMember(alias, member, seen); err != nil {
			return nil, err
		}
		seen[member] = struct{}{}
		members[index] = member
	}
	return members, nil
}

func (c *Config) validateMavenGroupMember(
	alias, member string,
	seen map[string]struct{},
) error {
	if member == "" {
		return fmt.Errorf("normalize maven group %q: member must not be empty", alias)
	}
	if _, duplicate := seen[member]; duplicate {
		return fmt.Errorf("normalize maven group %q: duplicate member %q", alias, member)
	}
	if _, nested := c.MavenGroups[member]; nested {
		return fmt.Errorf(
			"normalize maven group %q: nested group member %q is not supported",
			alias,
			member,
		)
	}
	if _, exists := c.Maven[member]; !exists {
		return fmt.Errorf("normalize maven group %q: unknown member %q", alias, member)
	}
	return nil
}

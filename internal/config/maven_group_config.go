package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/samber/lo"
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
	return slices.Sorted(maps.Keys(c.MavenGroups))
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

	seen := collectionset.NewSetWithCapacity[string](len(rawMembers))
	members, err := lo.MapErr(rawMembers, func(rawMember string, _ int) (string, error) {
		member := strings.TrimSpace(rawMember)
		if err := c.validateMavenGroupMember(alias, member, seen); err != nil {
			return "", err
		}
		seen.Add(member)
		return member, nil
	})
	if err != nil {
		return nil, fmt.Errorf("normalize maven group %q members: %w", alias, err)
	}
	return members, nil
}

func (c *Config) validateMavenGroupMember(
	alias, member string,
	seen *collectionset.Set[string],
) error {
	if member == "" {
		return fmt.Errorf("normalize maven group %q: member must not be empty", alias)
	}
	if seen.Contains(member) {
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

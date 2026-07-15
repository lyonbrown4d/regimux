package config_test

import (
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestNormalizeMavenGroups(t *testing.T) {
	cfg := config.Config{
		Maven: config.DependencyEcosystemConfig{
			"central": {Registry: "https://repo.maven.apache.org/maven2"},
			"spring":  {Registry: "https://repo.spring.io/release"},
		},
		MavenGroups: config.MavenGroupsConfig{
			" public ": {
				Members: []string{" spring ", "central"},
			},
		},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}

	group, ok := cfg.MavenGroup("public")
	if !ok {
		t.Fatal("normalized Maven group was not found")
	}
	if group.MetadataPolicy != config.MavenMetadataPolicyMerge {
		t.Fatalf("metadata policy = %q, want merge", group.MetadataPolicy)
	}
	if len(group.Members) != 2 || group.Members[0] != "spring" || group.Members[1] != "central" {
		t.Fatalf("members = %v, want [spring central]", group.Members)
	}
}

func TestNormalizeMavenGroupsRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name      string
		groups    config.MavenGroupsConfig
		wantError string
	}{
		{
			name: "empty members",
			groups: config.MavenGroupsConfig{
				"public": {},
			},
			wantError: "members must not be empty",
		},
		{
			name: "duplicate member",
			groups: config.MavenGroupsConfig{
				"public": {Members: []string{"central", " central "}},
			},
			wantError: "duplicate member",
		},
		{
			name: "unknown member",
			groups: config.MavenGroupsConfig{
				"public": {Members: []string{"missing"}},
			},
			wantError: "unknown member",
		},
		{
			name: "normalized route alias collision",
			groups: config.MavenGroupsConfig{
				" central ": {Members: []string{"central"}},
			},
			wantError: "alias conflicts",
		},
		{
			name: "nested group",
			groups: config.MavenGroupsConfig{
				"internal": {Members: []string{"central"}},
				"public":   {Members: []string{"internal"}},
			},
			wantError: "nested group",
		},
		{
			name: "invalid metadata policy",
			groups: config.MavenGroupsConfig{
				"public": {
					Members:        []string{"central"},
					MetadataPolicy: "fastest",
				},
			},
			wantError: "metadata_policy",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Config{
				Maven: config.DependencyEcosystemConfig{
					"central": {Registry: "https://repo.maven.apache.org/maven2"},
				},
				MavenGroups: test.groups,
			}
			err := cfg.Normalize()
			if err == nil {
				t.Fatal("expected normalization error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

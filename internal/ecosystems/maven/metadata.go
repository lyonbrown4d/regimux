package maven

import (
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
)

type mavenMetadataDocument struct {
	XMLName      xml.Name                 `xml:"metadata"`
	ModelVersion string                   `xml:"modelVersion,attr,omitempty"`
	GroupID      string                   `xml:"groupId,omitempty"`
	ArtifactID   string                   `xml:"artifactId,omitempty"`
	Version      string                   `xml:"version,omitempty"`
	Versioning   *mavenMetadataVersioning `xml:"versioning,omitempty"`
	Plugins      *mavenMetadataPlugins    `xml:"plugins,omitempty"`
}

type mavenMetadataVersioning struct {
	Latest      string                 `xml:"latest,omitempty"`
	Release     string                 `xml:"release,omitempty"`
	Versions    *mavenMetadataVersions `xml:"versions,omitempty"`
	LastUpdated string                 `xml:"lastUpdated,omitempty"`
}

type mavenMetadataVersions struct {
	Items []string `xml:"version"`
}

type mavenMetadataPlugins struct {
	Items []mavenMetadataPlugin `xml:"plugin"`
}

type mavenMetadataPlugin struct {
	Name       string `xml:"name,omitempty"`
	Prefix     string `xml:"prefix,omitempty"`
	ArtifactID string `xml:"artifactId,omitempty"`
}

type metadataMergeState struct {
	document          mavenMetadataDocument
	versionSet        *collectionset.Set[string]
	plugins           *collectionmapping.Map[string, mavenMetadataPlugin]
	latestCandidates  []string
	releaseCandidates []string
}

func parseMavenMetadata(data []byte) (mavenMetadataDocument, error) {
	var document mavenMetadataDocument
	if err := xml.Unmarshal(data, &document); err != nil {
		return mavenMetadataDocument{}, fmt.Errorf("unmarshal Maven metadata: %w", err)
	}
	if document.XMLName.Local != "metadata" {
		return mavenMetadataDocument{}, fmt.Errorf(
			"unexpected Maven metadata root %q",
			document.XMLName.Local,
		)
	}
	return document, nil
}

func mergeMavenMetadata(documents []mavenMetadataDocument) ([]byte, error) {
	if len(documents) == 0 {
		return nil, errors.New("merge Maven metadata: no documents")
	}

	state := newMetadataMergeState()
	for _, document := range documents {
		if err := state.mergeDocument(document); err != nil {
			return nil, err
		}
	}
	state.finalize()

	payload, err := xml.MarshalIndent(state.document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged Maven metadata: %w", err)
	}
	return append([]byte(xml.Header), payload...), nil
}

func newMetadataMergeState() *metadataMergeState {
	return &metadataMergeState{
		document: mavenMetadataDocument{
			XMLName: xml.Name{Local: "metadata"},
		},
		versionSet: collectionset.NewSet[string](),
		plugins:    collectionmapping.NewMap[string, mavenMetadataPlugin](),
	}
}

func (state *metadataMergeState) mergeDocument(document mavenMetadataDocument) error {
	if err := mergeMetadataIdentity(&state.document.GroupID, document.GroupID, "groupId"); err != nil {
		return err
	}
	if err := mergeMetadataIdentity(&state.document.ArtifactID, document.ArtifactID, "artifactId"); err != nil {
		return err
	}
	if err := mergeMetadataIdentity(&state.document.Version, document.Version, "version"); err != nil {
		return err
	}
	if state.document.ModelVersion == "" {
		state.document.ModelVersion = document.ModelVersion
	}
	mergeMetadataVersioning(state, document.Versioning)
	mergeMetadataPlugins(state.plugins, document.Plugins)
	return nil
}

func (state *metadataMergeState) finalize() {
	finalizeMetadataVersioning(
		&state.document,
		state.versionSet,
		state.latestCandidates,
		state.releaseCandidates,
	)
	finalizeMetadataPlugins(&state.document, state.plugins)
}

func mergeMetadataIdentity(current *string, incoming, field string) error {
	if incoming == "" {
		return nil
	}
	if *current == "" {
		*current = incoming
		return nil
	}
	if *current != incoming {
		return fmt.Errorf(
			"merge Maven metadata: conflicting %s values %q and %q",
			field,
			*current,
			incoming,
		)
	}
	return nil
}

func mergeMetadataVersioning(
	state *metadataMergeState,
	incoming *mavenMetadataVersioning,
) {
	if incoming == nil {
		return
	}
	versioning := ensureMetadataVersioning(&state.document)
	if incoming.LastUpdated > versioning.LastUpdated {
		versioning.LastUpdated = incoming.LastUpdated
	}
	appendMetadataCandidates(state, incoming)
	mergeMetadataVersions(state, incoming.Versions)
}

func ensureMetadataVersioning(document *mavenMetadataDocument) *mavenMetadataVersioning {
	if document.Versioning == nil {
		document.Versioning = &mavenMetadataVersioning{}
	}
	return document.Versioning
}

func appendMetadataCandidates(
	state *metadataMergeState,
	incoming *mavenMetadataVersioning,
) {
	if incoming.Latest != "" {
		state.latestCandidates = append(state.latestCandidates, incoming.Latest)
	}
	if incoming.Release == "" {
		return
	}
	state.latestCandidates = append(state.latestCandidates, incoming.Release)
	if !isSnapshotVersion(incoming.Release) {
		state.releaseCandidates = append(state.releaseCandidates, incoming.Release)
	}
}

func mergeMetadataVersions(
	state *metadataMergeState,
	incoming *mavenMetadataVersions,
) {
	if incoming == nil {
		return
	}
	for _, rawVersion := range incoming.Items {
		version := strings.TrimSpace(rawVersion)
		if version == "" {
			continue
		}
		state.versionSet.Add(version)
		state.latestCandidates = append(state.latestCandidates, version)
		if !isSnapshotVersion(version) {
			state.releaseCandidates = append(state.releaseCandidates, version)
		}
	}
}

func mergeMetadataPlugins(
	plugins *collectionmapping.Map[string, mavenMetadataPlugin],
	incoming *mavenMetadataPlugins,
) {
	if incoming == nil {
		return
	}
	for _, plugin := range incoming.Items {
		key := plugin.Prefix + "\x00" + plugin.ArtifactID
		plugins.GetOrSet(key, plugin)
	}
}

func finalizeMetadataVersioning(
	merged *mavenMetadataDocument,
	versionSet *collectionset.Set[string],
	latestCandidates []string,
	releaseCandidates []string,
) {
	if merged.Versioning == nil {
		return
	}
	versions := sortedMavenVersions(versionSet)
	if len(versions) > 0 {
		merged.Versioning.Versions = &mavenMetadataVersions{Items: versions}
	}
	merged.Versioning.Latest = maxMavenVersion(latestCandidates)
	merged.Versioning.Release = maxMavenVersion(releaseCandidates)
}

func sortedMavenVersions(versionSet *collectionset.Set[string]) []string {
	versions := versionSet.Values()
	sort.SliceStable(versions, func(left, right int) bool {
		comparison := compareMavenVersions(versions[left], versions[right])
		if comparison == 0 {
			return versions[left] < versions[right]
		}
		return comparison < 0
	})
	return versions
}

func finalizeMetadataPlugins(
	merged *mavenMetadataDocument,
	pluginSet *collectionmapping.Map[string, mavenMetadataPlugin],
) {
	if pluginSet.IsEmpty() {
		return
	}
	plugins := sortedMavenPlugins(pluginSet)
	merged.Plugins = &mavenMetadataPlugins{Items: plugins}
}

func sortedMavenPlugins(pluginSet *collectionmapping.Map[string, mavenMetadataPlugin]) []mavenMetadataPlugin {
	plugins := pluginSet.Values()
	sort.SliceStable(plugins, func(left, right int) bool {
		if plugins[left].Prefix != plugins[right].Prefix {
			return plugins[left].Prefix < plugins[right].Prefix
		}
		if plugins[left].ArtifactID != plugins[right].ArtifactID {
			return plugins[left].ArtifactID < plugins[right].ArtifactID
		}
		return plugins[left].Name < plugins[right].Name
	})
	return plugins
}

func maxMavenVersion(versions []string) string {
	maximum := ""
	for _, version := range versions {
		if version == "" {
			continue
		}
		if maximum == "" || compareMavenVersions(version, maximum) > 0 {
			maximum = version
		}
	}
	return maximum
}

func isSnapshotVersion(version string) bool {
	return strings.Contains(strings.ToUpper(version), "SNAPSHOT")
}

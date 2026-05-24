// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// LabelInputs supplies the values an OCI label set wants on top of the
// rule-derived primary version. RepoMetadata fields are typically
// fetched from the provider; description/license overrides take
// precedence when provided.
type LabelInputs struct {
	ImageName   string // ghcr.io/owner/repo or similar
	RepoURL     string // canonical web URL of the repo
	SHA         string // full commit SHA (org.opencontainers.image.revision)
	Primary     string // chosen primary tag (org.opencontainers.image.version)
	Description string // already-resolved description (override or fetched)
	License     string // already-resolved SPDX id
	CreatedAt   time.Time
}

// Label is a single OCI label entry. Slice-of-struct (rather than map)
// preserves emission order, which the bats tests assert.
type Label struct {
	Key   string
	Value string
}

// String renders the label as `key=value` (one line of TAG_RULES output).
func (l Label) String() string { return l.Key + "=" + l.Value }

// BuildLabels assembles the org.opencontainers.image.* label set.
// Title is the last segment of the image name (e.g. ghcr.io/o/repo → repo).
// CreatedAt is rendered as RFC3339 UTC.
func BuildLabels(in LabelInputs) []Label {
	title := in.ImageName
	if i := strings.LastIndex(title, "/"); i != -1 {
		title = title[i+1:]
	}

	created := in.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")

	return []Label{
		{Key: "org.opencontainers.image.title", Value: title},
		{Key: "org.opencontainers.image.description", Value: in.Description},
		{Key: "org.opencontainers.image.url", Value: in.RepoURL},
		{Key: "org.opencontainers.image.source", Value: in.RepoURL},
		{Key: "org.opencontainers.image.version", Value: in.Primary},
		{Key: "org.opencontainers.image.created", Value: created},
		{Key: "org.opencontainers.image.revision", Value: in.SHA},
		{Key: "org.opencontainers.image.licenses", Value: in.License},
	}
}

// PrimaryVersion picks the tag with the highest priority. Ties resolve
// to the first declared, mirroring `sort -k1,1nr -s | head -n1` in bash.
// Returns "" when applied is empty.
func PrimaryVersion(applied []AppliedTag) string {
	if len(applied) == 0 {
		return ""
	}
	// Stable sort by priority descending; first wins.
	sorted := make([]AppliedTag, len(applied))
	copy(sorted, applied)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	return sorted[0].Tag
}

// FormatTags prefixes each AppliedTag with `image:` and returns the
// declaration-order tag list. Empty AppliedTag (silently-skipped rule)
// is filtered out by the caller before this point.
func FormatTags(image string, applied []AppliedTag) []string {
	out := make([]string, 0, len(applied))
	for _, a := range applied {
		out = append(out, image+":"+a.Tag)
	}

	return out
}

// JSONOutput is the JSON shape mirroring docker/metadata-action: a tags
// array and a labels object.
type JSONOutput struct {
	Tags   []string          `json:"tags"`
	Labels map[string]string `json:"labels"`
}

// BuildJSONOutput composes the {"tags":[...],"labels":{...}} document.
// labels=nil yields {} (not omitted) to match the action's shape.
func BuildJSONOutput(tags []string, labels []Label) JSONOutput {
	out := JSONOutput{Tags: tags}
	if out.Tags == nil {
		out.Tags = []string{}
	}

	out.Labels = make(map[string]string, len(labels))
	for _, l := range labels {
		out.Labels[l.Key] = l.Value
	}

	return out
}

// MarshalJSON returns the canonical JSON encoding of o.
func (o JSONOutput) MarshalJSON() ([]byte, error) {
	type alias JSONOutput

	return json.Marshal(alias(o))
}

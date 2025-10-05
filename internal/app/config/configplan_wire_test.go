// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// wireGroup is one planned entry as the workflow reads it from the JSON.
type wireGroup []struct {
	Name string `json:"name"`
}

func (g wireGroup) names() []string {
	names := make([]string, 0, len(g))
	for _, entry := range g {
		names = append(names, entry.Name)
	}

	return names
}

// TestEmitConfigPlan_WireGroupsAreExactAndLaterEntriesStayDistinct reads the
// emitted config-plan JSON as a workflow would, without the Go plan type or
// its version constant: the version is the literal 1 on the wire, every
// artifact group has exactly the names this fixture puts in it (and an empty
// array, not null, where it puts none), and the second container keeps its own
// name, sources, file, context and platforms rather than echoing the first.
func TestEmitConfigPlan_WireGroupsAreExactAndLaterEntriesStayDistinct(t *testing.T) {
	t.Parallel()

	path := writeYAML(t, `
artifacts:
  - name: my-lib
    project-type: maven
    build-type: library
    publish-to: [maven-central, forge-packages]
    require-authorization: true
  - name: go-cli
    project-type: go
    config:
      build-mode: artifact-first
  - name: go-service
    project-type: go
    working-directory: services/go-service
    config:
      build-mode: container-first
  - name: web
    project-type: npm
    working-directory: web
    publish-to: [forge-packages]
containers:
  - name: go-service
    from: [go-cli, go-service]
    container-file: Containerfile
  - name: web-image
    from: [web]
    container-file: web/Containerfile
    context: web
    platforms: linux/amd64,linux/arm64
`)

	sink := fakeoutputsink.New(t)
	require.NoError(t, appconfig.EmitConfigPlan(t.Context(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}))

	var wire struct {
		Version                 json.RawMessage      `json:"version"`
		AnyRequireAuthorization bool                 `json:"any_require_authorization"`
		Artifacts               map[string]wireGroup `json:"artifacts"`
		Containers              struct {
			HasContainers bool `json:"has_containers"`
			All           []struct {
				Name           string   `json:"name"`
				From           []string `json:"from"`
				ContainerFile  string   `json:"container_file"`
				Context        string   `json:"context"`
				Platforms      string   `json:"platforms"`
				GoArtifactName string   `json:"go_artifact_name"`
			} `json:"all"`
		} `json:"containers"`
	}

	raw := sink.Single("config-plan-json")
	require.NoError(t, json.Unmarshal([]byte(raw), &wire))
	require.JSONEq(t, `1`, string(wire.Version))
	require.True(t, wire.AnyRequireAuthorization)

	var groups map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &struct {
		Artifacts *map[string]json.RawMessage `json:"artifacts"`
	}{&groups}))

	for group, body := range groups {
		require.NotEqual(t, "null", string(body), "group %s is null", group)
	}

	got := make(map[string][]string, len(wire.Artifacts))
	for group, entries := range wire.Artifacts {
		got[group] = entries.names()
	}

	require.Equal(t, map[string][]string{
		"all":                   {"my-lib", "go-cli", "go-service", "web"},
		"maven":                 {"my-lib"},
		"npm":                   {"web"},
		"gradle":                {},
		"gradle_android":        {},
		"xcode_ios":             {},
		"python":                {},
		"go":                    {"go-cli", "go-service"},
		"cargo":                 {},
		"meta":                  {},
		"go_artifact_first":     {"go-cli"},
		"go_container_first":    {"go-service"},
		"cargo_artifact_first":  {},
		"cargo_container_first": {},
		"forge_packages":        {"my-lib", "web"},
		"maven_central":         {"my-lib"},
		"google_play":           {},
		"npmjs":                 {},
	}, got)

	require.True(t, wire.Containers.HasContainers)
	require.Len(t, wire.Containers.All, 2)

	first, second := wire.Containers.All[0], wire.Containers.All[1]
	require.Equal(t, []string{"go-service", "go-cli", "go-service", "Containerfile", ".", "linux/amd64", "go-cli"},
		append(append([]string{first.Name}, first.From...), first.ContainerFile, first.Context, first.Platforms, first.GoArtifactName))
	require.Equal(t, []string{"web-image", "web", "web/Containerfile", "web", "linux/amd64,linux/arm64", ""},
		append(append([]string{second.Name}, second.From...), second.ContainerFile, second.Context, second.Platforms, second.GoArtifactName))
}

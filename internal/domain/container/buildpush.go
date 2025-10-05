// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

// BuildPushPlatformBuild describes one platform entry in the build-push-oci-image action's JSON contract.
type BuildPushPlatformBuild struct {
	Platform  string   `json:"platform"`
	BuildArgs []string `json:"build-args"`
}

// BuildPushManifestBuildRequest is the buildah-level request for one platform build into a shared manifest list.
type BuildPushManifestBuildRequest struct {
	AuthFile        string
	Manifest        string
	TLSVerify       bool
	SourceDateEpoch string
	Platform        string
	Containerfile   string
	BuildArgs       []string
	Labels          []string
	Context         string
}

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

// SignerImageBuildToolRequest is the buildah-level request for one
// architecture build of a two-phase multi-arch image.
type SignerImageBuildToolRequest struct {
	AuthFile      string
	Platform      string
	SourceURL     string
	Revision      string
	LocalImage    string
	Containerfile string
	Context       string
	Title         string // org.opencontainers.image.title label; empty omits the label
}

// SignerImageManifestAddToolRequest is the buildah-level request for adding one
// digest-pinned architecture image to a local manifest list.
type SignerImageManifestAddToolRequest struct {
	AuthFile      string
	Arch          string
	LocalManifest string
	Ref           string
}

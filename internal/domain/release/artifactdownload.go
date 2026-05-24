// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

// ArtifactDownloadInput identifies one CI artifact download operation.
type ArtifactDownloadInput struct {
	RunID      string
	Repository string
	Name       string
	Dir        string
}

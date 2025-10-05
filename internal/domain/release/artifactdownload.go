// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

// ArtifactDownloadInput identifies one CI artifact download operation.
type ArtifactDownloadInput struct {
	RunID      string
	Repository string
	Name       string
	Dir        string
}

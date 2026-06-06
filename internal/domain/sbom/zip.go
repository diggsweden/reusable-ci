// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package sbom holds pure SBOM-related domain helpers: canonical
// filenames, format decisions over a SBOMPolicy, and the like. SBOM
// generation itself shells out to syft via adapter/syft.
package sbom

import "fmt"

// ZipName returns the canonical SBOM zip filename, e.g.
//
//	ZipName("my-app", "1.2.3") = "my-app-1.2.3-sboms.zip"
func ZipName(projectName, version string) string {
	return fmt.Sprintf("%s-%s-sboms.zip", projectName, version)
}

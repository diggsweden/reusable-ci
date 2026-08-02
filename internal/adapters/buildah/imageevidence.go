// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah

import (
	"context"
	"io"
)

// ExportLocalImageToLayout exports an image from buildah's local storage to an OCI layout.
func (a *Adapter) ExportLocalImageToLayout(ctx context.Context, imageRef, layoutDir string, out io.Writer) error {
	return a.run(ctx, out, a.global("push", imageRef, "oci:"+layoutDir+":scan")...)
}

// ExportLocalManifestToLayout exports a local Buildah manifest list to an OCI layout.
func (a *Adapter) ExportLocalManifestToLayout(ctx context.Context, manifest, layoutDir string, out io.Writer) error {
	return a.run(ctx, out, a.global("manifest", "push", "--all", manifest, "oci:"+layoutDir+":scan")...)
}

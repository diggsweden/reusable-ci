// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

// OCILabels is the caller-supplied org.opencontainers.image.* label
// identity for a release image build. It is the one home for the flat
// label fields the build-push and release-labels flows share; app-layer
// inputs embed it instead of re-declaring each field.
type OCILabels struct {
	Title         string
	Description   string
	Licenses      string
	Vendor        string
	Authors       string
	Documentation string
	RefName       string
	Version       string
	Revision      string
}

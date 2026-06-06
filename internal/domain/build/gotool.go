// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import "io"

// GoRunInput is one go/cyclonedx-gomod command invocation.
type GoRunInput struct {
	Dir    string
	Env    []string
	Args   []string
	Stdout io.Writer
	Stderr io.Writer
}

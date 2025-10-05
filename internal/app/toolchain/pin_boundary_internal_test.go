// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestChangelogPinBoundary_ExactVersions(t *testing.T) {
	t.Parallel()

	for _, pin := range []string{"1.2.3", "latest", "v1.2.3", "1.2", " 1.2.3", "1.2.3\n", "1.2.3-rc1", "1.2.3+build", "1.2.3/other"} {
		_, _, _, err := changelogRendererSelector(InstallChangelogRendererInput{Backend: gitCliffBin, GitCliffVersion: pin})
		if pin == "1.2.3" {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("pin=%q err=%v", pin, err)
		}
	}
}

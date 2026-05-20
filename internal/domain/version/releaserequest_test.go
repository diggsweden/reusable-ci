// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

func TestReleaseRequestVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		ref     string
		wantTag string
		wantOK  bool
	}{
		{"short ref-name", "release-request/v3.5.7", "v3.5.7", true},
		{"fully-qualified", "refs/tags/release-request/v3.5.7", "v3.5.7", true},
		{"prerelease", "release-request/v1.0.0-rc.1", "v1.0.0-rc.1", true},
		{"final tag is not a request", "v3.5.7", "", false},
		{"qualified final tag", "refs/tags/v3.5.7", "", false},
		{"branch", "refs/heads/main", "", false},
		{"empty", "", "", false},
		{"prefix only, no version", "release-request/", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tag, ok := version.ReleaseRequestVersion(tc.ref)
			if tag != tc.wantTag || ok != tc.wantOK {
				t.Errorf("ReleaseRequestVersion(%q) = (%q, %v), want (%q, %v)", tc.ref, tag, ok, tc.wantTag, tc.wantOK)
			}
		})
	}
}

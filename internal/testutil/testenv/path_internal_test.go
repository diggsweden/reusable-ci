// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package testenv

import (
	"fmt"
	"testing"
)

type fatalProbe struct {
	testing.TB

	fatal string
}

func (p *fatalProbe) Helper() {}

func (p *fatalProbe) Fatalf(format string, args ...any) { p.fatal = fmt.Sprintf(format, args...) }

// TestEnvPath_RefusesNamesOutsideTheTempDirectory refuses absolute and climbing
// names and joins local ones below the isolated temp directory.
func TestEnvPath_RefusesNamesOutsideTheTempDirectory(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		parts  []string
		refuse bool
	}{
		{[]string{"x", "y"}, false},
		{[]string{"/tmp/host"}, true},
		{[]string{"..", "home"}, true},
	} {
		probe := &fatalProbe{TB: t}
		env := &Env{t: probe, Temp: t.TempDir()}

		env.Path(tc.parts...)

		if refused := probe.fatal != ""; refused != tc.refuse {
			t.Errorf("Path(%q) refused = %t, want %t", tc.parts, refused, tc.refuse)
		}
	}
}

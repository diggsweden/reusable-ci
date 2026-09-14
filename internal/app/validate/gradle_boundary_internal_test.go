// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import "testing"

func TestGradleSettingBoundary_IndependentNamesAndValues(t *testing.T) {
	t.Parallel()

	for _, setting := range []struct{ key, value string }{
		{"preserveFileTimestamps", "false"}, {"reproducibleFileOrder", "true"},
		{"isPreserveFileTimestamps", "false"}, {"isReproducibleFileOrder", "true"},
	} {
		for _, tc := range []struct {
			line string
			want bool
		}{
			{setting.key + " = " + setting.value, true},
			{"my" + setting.key + " = " + setting.value, false},
			{setting.key + " = " + setting.value + "X", false},
			{setting.key + " = " + setting.value + ";", true},
		} {
			if got := lineAssignsGradleSetting(tc.line, setting.key, setting.value); got != tc.want {
				t.Errorf("line=%q got=%v want=%v", tc.line, got, tc.want)
			}
		}
	}
}

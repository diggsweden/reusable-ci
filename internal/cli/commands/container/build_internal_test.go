// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestSplitSecretSpecs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		value   string
		want    []string
		wantErr error
	}{
		{name: "empty", value: "", want: nil},
		{name: "legacy lines", value: "id=a,src=/tmp/a\nid=b,src=/tmp/b", want: []string{"id=a,src=/tmp/a", "id=b,src=/tmp/b"}},
		{name: "scalar JSON", value: `["id=a,src=/tmp/a","id=b,src=/tmp/path with spaces/b"]`, want: []string{"id=a,src=/tmp/a", "id=b,src=/tmp/path with spaces/b"}},
		{name: "malformed JSON", value: `["id=a"`, wantErr: errs.ErrMalformedInput},
		{name: "multiline JSON entry", value: `["id=a,src=/tmp/a\nforged"]`, wantErr: errs.ErrValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := splitSecretSpecs(test.value)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("err = %v, want %v", err, test.wantErr)
			}

			if !slices.Equal(got, test.want) {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

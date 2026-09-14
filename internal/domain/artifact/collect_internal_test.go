// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestValidateUploadAggregateBoundsCountAndUncompressedBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []UploadEntry
		wantErr bool
	}{
		{name: "empty"},
		{name: "below limits", entries: []UploadEntry{{RelPath: "a", Size: 1}}},
		{name: "exact byte limit", entries: []UploadEntry{{RelPath: "a", Size: MaxTotalBytes / 2}, {RelPath: "b", Size: MaxTotalBytes / 2}}},
		{name: "over byte limit", entries: []UploadEntry{{RelPath: "a", Size: MaxTotalBytes}, {RelPath: "b", Size: 1}}, wantErr: true},
		{name: "exact file limit", entries: make([]UploadEntry, MaxFileCount)},
		{name: "over file limit", entries: make([]UploadEntry, MaxFileCount+1), wantErr: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateUploadAggregate(testCase.entries)
			if testCase.wantErr && !errors.Is(err, errs.ErrValidation) || !testCase.wantErr && err != nil {
				t.Fatalf("validateUploadAggregate error = %v, want validation=%v", err, testCase.wantErr)
			}
		})
	}
}

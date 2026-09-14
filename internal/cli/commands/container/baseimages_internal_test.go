// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestBaseImagesGroupExposesWorkflowCommands(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, cmd := range baseImagesGroup().Commands {
		found[cmd.Name] = true
	}

	for _, name := range []string{"validate", "promote", "cleanup", "prune", "freshness"} {
		if !found[name] {
			t.Errorf("base-images command %q not exposed", name)
		}
	}
}

// TestRequireRegistrySurfaceMatches pins the refusals that stop a
// --local-registry setting contradicting where the bases actually live.
//
// Neither mismatch fails legibly on its own: a forge package API asked to
// delete from a loopback repository returns a confusing 404, and the OCI
// adapter pointed at a forge registry would refuse every shared manifest
// rather than deleting the version it was asked for. Both are configuration
// errors and should read as such.
func TestRequireRegistrySurfaceMatches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		local       bool
		common      baseImagesCommon
		wantRefusal bool
	}{
		{
			name:   "loopback repository with the flag is the local topology",
			local:  true,
			common: baseImagesCommon{ExpectedRepository: "localhost:5000/owner/project-base"},
		},
		{
			name:        "loopback repository without the flag cannot use a package API",
			local:       false,
			common:      baseImagesCommon{ExpectedRepository: "127.0.0.1:5000/owner/project-base"},
			wantRefusal: true,
		},
		{
			name:   "forge repository without the flag is the remote topology",
			local:  false,
			common: baseImagesCommon{ExpectedRepository: "codefloe.com/owner/project-base", ServerURL: "https://codefloe.com"},
		},
		{
			name:        "forge repository with the flag would bypass the package API",
			local:       true,
			common:      baseImagesCommon{ExpectedRepository: "codefloe.com/owner/project-base", ServerURL: "https://codefloe.com"},
			wantRefusal: true,
		},
		{
			// A private registry that is neither loopback nor the forge: the
			// runner-pool topology. The flag is the only way to reach it.
			name:   "private non-forge registry with the flag is allowed",
			local:  true,
			common: baseImagesCommon{ExpectedRepository: "bases.ci.internal:5000/owner/project-base", ServerURL: "https://codefloe.com"},
		},
		{
			name:   "no expected repository yet is not a mismatch",
			local:  true,
			common: baseImagesCommon{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := requireRegistrySurfaceMatches(tc.local, tc.common)

			switch {
			case tc.wantRefusal && err == nil:
				t.Fatal("requireRegistrySurfaceMatches() = nil, want a refusal")
			case tc.wantRefusal && !errors.Is(err, errs.ErrUsage):
				t.Errorf("error = %v, want errs.ErrUsage", err)
			case !tc.wantRefusal && err != nil:
				t.Errorf("requireRegistrySurfaceMatches() = %v, want nil", err)
			}
		})
	}
}

// TestReadBaseImagesFlavors_RequiresAPath pins the absence of a default.
//
// The flag used to default to one consumer's layout, which was wrong for every
// other project and wrong silently: the failure named a path the reader had
// never chosen. Requiring it makes the engine ask rather than guess, and the
// consumer-facing shim keeps whatever convention it likes.
func TestReadBaseImagesFlavors_RequiresAPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"", "   "} {
		got, err := readBaseImagesFlavors(path)
		if err == nil {
			t.Fatalf("readBaseImagesFlavors(%q) = %v, want a refusal rather than a guessed path", path, got)
		}

		if !errors.Is(err, errs.ErrUsage) {
			t.Errorf("error = %v, want errs.ErrUsage so the exit code reads as misuse", err)
		}
	}
}

// TestReadBaseImagesFlavors_TrimsEachLine pins that a flavors file with CRLF
// endings or trailing spaces yields bare flavor names. The blank/comment check
// already trimmed each line, but the raw line was what got appended, so its
// "\r" or " " reached the flavor pattern and the verify verb refused a file
// that listed nothing but valid flavors.
func TestReadBaseImagesFlavors_TrimsEachLine(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("flavors.txt", []byte("alpine \r\n# comment\r\n\r\ndebian\r\n"))
	fsys.Chdir()

	got, err := readBaseImagesFlavors("flavors.txt")
	if err != nil {
		t.Fatalf("readBaseImagesFlavors() error = %v", err)
	}

	if want := []string{"alpine", "debian"}; !slices.Equal(got, want) {
		t.Errorf("flavors = %q, want %q", got, want)
	}
}

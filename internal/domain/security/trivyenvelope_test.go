// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// Trivy's native JSON is an external contract this repository does not control.
// The transforms here read a subset of it, and `encoding/json` ignores every
// key they do not name — which is the right default for a format that gains
// fields, and the wrong one for a format that RENAMES them.
//
// A rename is the case worth thinking about, because it does not look like a
// failure. If `Results[].Vulnerabilities` moved, every report would still parse,
// every scan would find nothing, and the pipeline would report a clean image.
// No error, no empty output, no signal at all.
//
// The honest thing to say about that risk is what this file says: the consumed
// subset is written down against the Trivy version the repository pins, and
// changing that version without revisiting the envelope fails. That is a
// process guarantee, not proof of compatibility — proving compatibility needs
// output from a real Trivy run, which needs a vulnerability database download
// and belongs to the separately authorized tier. Synthetic fixtures cannot
// stand in for it and this does not pretend they do.

// verifiedTrivyVersion is the version whose native output the consumed field
// inventory below was checked against. Bumping trivy in .mise.toml without
// updating this fails TestTrivyEnvelope_IsPinnedToTheVerifiedVersion.
const verifiedTrivyVersion = "0.74.0"

// consumedTrivyFields is every key the transforms read, by JSON path. Adding a
// field to a struct in trivy.go without adding it here fails, so the inventory
// cannot fall behind the code it describes.
func consumedTrivyFields() []string {
	return []string{
		"ArtifactName",
		"Metadata",
		"Metadata.OS",
		"Metadata.OS.Family",
		"Metadata.OS.Name",
		"Results",
		"Results.Class",
		"Results.Target",
		"Results.Vulnerabilities",
		"Results.Vulnerabilities.Description",
		"Results.Vulnerabilities.FixedVersion",
		"Results.Vulnerabilities.InstalledVersion",
		"Results.Vulnerabilities.PkgName",
		"Results.Vulnerabilities.PrimaryURL",
		"Results.Vulnerabilities.References",
		"Results.Vulnerabilities.Severity",
		"Results.Vulnerabilities.Title",
		"Results.Vulnerabilities.VulnerabilityID",
	}
}

func TestTrivyEnvelope_InventoryMatchesTheStructsThatDecodeIt(t *testing.T) {
	t.Parallel()

	got := jsonFieldPaths(reflect.TypeOf(security.TrivyReport{}), "")
	want := consumedTrivyFields()

	sort.Strings(got)
	sort.Strings(want)

	if len(got) == 0 {
		t.Fatal("no JSON fields found on TrivyReport; the reflection, not the type, is what was measured")
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the consumed-field inventory and the decoding structs disagree.\n got:  %v\n want: %v\n\n"+
			"If a field was added, record it here and say which Trivy version it was seen in. If one was removed, "+
			"check whether the transforms stopped needing it or whether Trivy stopped emitting it — the second is "+
			"the case that reads as a clean scan.", got, want)
	}
}

func TestTrivyEnvelope_IsPinnedToTheVerifiedVersion(t *testing.T) {
	t.Parallel()

	body := string(reporoot.ReadFile(t, ".mise.toml"))

	match := regexp.MustCompile(`"aqua:aquasecurity/trivy"\s*=\s*"([^"]+)"`).FindStringSubmatch(body)
	if match == nil {
		t.Fatal("no trivy pin found in .mise.toml; the regexp, not the pin, is what was measured")
	}

	if match[1] != verifiedTrivyVersion {
		t.Errorf(".mise.toml pins trivy %s but the consumed envelope was verified against %s.\n"+
			"Re-check the native output of the new version against consumedTrivyFields — a renamed key parses "+
			"cleanly and reports no vulnerabilities — then update verifiedTrivyVersion.", match[1], verifiedTrivyVersion)
	}
}

// jsonFieldPaths walks a struct's json tags into dotted paths, descending
// through pointers and slices so nested envelope fields are named the way a
// reader of Trivy's output would name them.
func jsonFieldPaths(typ reflect.Type, prefix string) []string {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return nil
	}

	var paths []string

	for i := range typ.NumField() {
		field := typ.Field(i)

		tag, ok := field.Tag.Lookup("json")
		if !ok {
			continue
		}

		name, _, _ := strings.Cut(tag, ",")
		if name == "-" || name == "" {
			continue
		}

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		paths = append(paths, path)
		paths = append(paths, jsonFieldPaths(field.Type, path)...)
	}

	return paths
}

// The repository must not carry a Trivy report fixture that claims to be native
// output. A recorded native envelope is evidence; a hand-written subset with the
// same name is a synthetic document that looks like one, and the difference
// stops being visible the moment it is committed.
func TestTrivyEnvelope_NoFixtureClaimsToBeNativeOutput(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	checked := 0

	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return walkErr
		}

		if !strings.Contains(path, "testdata") {
			return nil
		}

		checked++

		body, readErr := os.ReadFile(path) //nolint:gosec // repository fixture under a walked testdata directory.
		if readErr != nil {
			return readErr
		}

		// SchemaVersion is the field Trivy stamps its own output with and the
		// transforms never read. A fixture carrying one is presenting itself
		// as a recording rather than a constructed example.
		if strings.Contains(string(body), `"SchemaVersion"`) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s carries SchemaVersion, presenting itself as recorded native Trivy output. "+
				"If it is a recording, say which version produced it; if it is constructed, drop the field so it "+
				"cannot be mistaken for compatibility evidence.", rel)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if checked == 0 {
		t.Skip("no JSON testdata fixtures under internal/; nothing to classify")
	}
}

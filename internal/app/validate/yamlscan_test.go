// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestYAMLMappingScalarValues_IgnoresCommentsAndResolvesAliases(t *testing.T) {
	t.Parallel()

	sha := "1111111111111111111111111111111111111111"
	body := []byte(`# uses: example/ci/action@2222222222222222222222222222222222222222
x-step: &shared
  uses: example/ci/action@` + sha + `
jobs:
  build:
    steps:
      - *shared
`)

	values, err := yamlMappingScalarValues(body, "uses")
	if err != nil {
		t.Fatal(err)
	}

	if len(values) != 1 || pinnedSubjectSHA(values[0], "example/ci") != sha {
		t.Fatalf("uses values = %v", values)
	}
}

func TestYAMLScalarAliasBoundary_KeysAndValues(t *testing.T) {
	t.Parallel()

	body := []byte("x-value: &value example/ci/action@first\nx-key: &key uses\njobs:\n  build:\n    steps:\n      - uses: *value\n      - *key: example/ci/action@second\n")

	values, err := yamlMappingScalarValues(body, "uses")
	if err != nil || !slices.Equal(values, []string{"example/ci/action@first", "example/ci/action@second"}) {
		t.Fatalf("values=%v err=%v", values, err)
	}
}

func TestCollectPinReachabilitySHAs_OnlyReadsUsesFields(t *testing.T) {
	t.Parallel()

	sha := "1111111111111111111111111111111111111111"
	other := "2222222222222222222222222222222222222222"
	file := filepath.Join(t.TempDir(), "release.yml")

	body := `# uses: example/ci/action@` + other + `
env:
  EXAMPLE: example/ci/action@` + other + `
jobs:
  release:
    uses: example/ci/.github/workflows/release.yml@` + sha + "\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := collectPinReachabilitySHAs([]string{file}, "example/ci")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != sha {
		t.Fatalf("pins = %v, want [%s]", got, sha)
	}
}

// A sibling repository whose name extends the subject is a different
// repository: its pin must not be collected as the subject's, and a
// subject nested under a host or owner prefix is still recognised.
func TestPinnedSubjectSHA_RequiresSubjectAsWholePathComponents(t *testing.T) {
	t.Parallel()

	sha := "1111111111111111111111111111111111111111"

	cases := map[string]struct {
		uses string
		want string
	}{
		"sibling repository extending the subject": {"example/ci-extras/.github/workflows/x.yml@" + sha, ""},
		"subject as a suffix of another owner":     {"myexample/ci/.github/workflows/x.yml@" + sha, ""},
		"plain forge shorthand":                    {"example/ci/.github/workflows/x.yml@" + sha, sha},
		"subject pinned directly":                  {"example/ci@" + sha, sha},
		"full URL form":                            {"https://codeberg.org/example/ci/.github/workflows/x.yml@" + sha, sha},
		"sibling first, subject later":             {"example/ci-extras/example/ci/x.yml@" + sha, sha},
		"branch ref is not a pin":                  {"example/ci/.github/workflows/x.yml@main", ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := pinnedSubjectSHA(tc.uses, "example/ci"); got != tc.want {
				t.Fatalf("pinnedSubjectSHA(%q) = %q, want %q", tc.uses, got, tc.want)
			}
		})
	}
}

// TestPinnedSubjectSHA_EnforcesTheSHA1Grammar covers the pin grammar on its
// own, independently of where the subject sits in the value.
//
// The subject-component cases above all pin an all-ones SHA or the branch name
// "main", so they separate 40 characters from 4 and nothing else. Removing the
// hexadecimal check from isSHA1 entirely leaves every one of them passing: the
// only property they need is length. That matters because the check is what
// distinguishes an immutable commit pin from a mutable ref, which is the whole
// point of requiring a pin — a 40-character tag or branch name is a legal Git
// ref, and treating one as a pin would report a mutable reference as immutable.
func TestPinnedSubjectSHA_EnforcesTheSHA1Grammar(t *testing.T) {
	t.Parallel()

	const (
		hex40 = "0123456789abcdef0123456789abcdef01234567"
		hex39 = "0123456789abcdef0123456789abcdef0123456"
		hex41 = "0123456789abcdef0123456789abcdef012345678"
	)

	for name, tc := range map[string]struct {
		ref  string
		want string
		why  string
	}{
		"exactly 40 hexadecimal characters is a pin": {hex40, hex40, "the canonical Git object name"},
		"39 characters is not a pin":                 {hex39, "", "an abbreviated object name is ambiguous and must not be accepted"},
		"41 characters is not a pin":                 {hex41, "", "longer than any Git SHA-1"},
		"a non-hex character at the end":             {"0123456789abcdef0123456789abcdef0123456g", "", "'g' is outside the hexadecimal alphabet"},
		"a non-hex character at the start":           {"z123456789abcdef0123456789abcdef01234567", "", "the first byte is checked like every other"},
		// Git renders object names in lowercase and every forge writes pins
		// that way, so uppercase is refused rather than normalised. Refusing
		// is the safe direction: the caller is told the ref is not a pin and
		// fails closed, where accepting would mean this validator and the
		// forge disagreeing about what the value is.
		"uppercase hexadecimal is not a pin": {"0123456789ABCDEF0123456789ABCDEF01234567", "", "pins are lowercase; refusing fails closed"},
		"a 40-character branch name is not a pin": {
			"release-branch-with-a-name-forty-chars-x", "",
			"length alone cannot decide this: a 40-character ref is legal and mutable",
		},
		"an empty ref is not a pin": {"", "", "uses: value ending in @ pins nothing"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			uses := "example/ci/.github/workflows/x.yml@" + tc.ref
			if got := pinnedSubjectSHA(uses, "example/ci"); got != tc.want {
				t.Errorf("pinnedSubjectSHA(%q) = %q, want %q: %s", uses, got, tc.want, tc.why)
			}
		})
	}
}

// TestPinnedSubjectSHA_DegenerateSubjects covers the two subject values that
// have no pin to find, so the caller gets "" rather than a panic or a match
// against the wrong part of the value.
func TestPinnedSubjectSHA_DegenerateSubjects(t *testing.T) {
	t.Parallel()

	const sha = "0123456789abcdef0123456789abcdef01234567"

	for name, tc := range map[string]struct {
		uses, subject string
	}{
		"an empty subject matches nothing":       {"example/ci/x.yml@" + sha, ""},
		"the subject is the whole value, no ref": {"example/ci", "example/ci"},
		"the subject is absent from the value":   {"other/repo/x.yml@" + sha, "example/ci"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := pinnedSubjectSHA(tc.uses, tc.subject); got != "" {
				t.Errorf("pinnedSubjectSHA(%q, %q) = %q, want %q", tc.uses, tc.subject, got, "")
			}
		})
	}
}

// TestCollectPinReachabilitySHAs_DeduplicatesAcrossFiles proves the collector
// reads every file it is given and reports each distinct pin once.
//
// The existing collector test passes a single file, so "reads the first file"
// and "reads all files" are the same behaviour to it, and a duplicate pin and
// two distinct pins are indistinguishable from one. Both matter to the caller:
// it refuses when a directory pins the subject at more than one SHA, so
// collapsing two into one would hide a mixed-pin repository, and reporting the
// same SHA twice would invent one.
func TestCollectPinReachabilitySHAs_DeduplicatesAcrossFiles(t *testing.T) {
	t.Parallel()

	const (
		first  = "1111111111111111111111111111111111111111"
		second = "2222222222222222222222222222222222222222"
	)

	dir := t.TempDir()

	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		return path
	}

	// The same pin twice in one file, the same pin again in a second file,
	// and one distinct pin that only the second file carries.
	a := write("a.yml", "jobs:\n  one:\n    uses: example/ci/.github/workflows/x.yml@"+first+
		"\n  two:\n    uses: example/ci/.github/workflows/y.yml@"+first+"\n")
	b := write("b.yml", "jobs:\n  three:\n    uses: example/ci/.github/workflows/x.yml@"+first+
		"\n  four:\n    uses: example/ci/.github/workflows/z.yml@"+second+"\n")

	got, err := collectPinReachabilitySHAs([]string{a, b}, "example/ci")
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{first, second}; !slices.Equal(got, want) {
		t.Errorf("pins = %v, want %v (sorted, deduplicated, and the second file must be read)", got, want)
	}
}

// TestCollectPinReachabilitySHAs_ReportsWhichFileFailed keeps the second file
// distinguishable when it is the broken one. A collector that stopped after the
// first file would report success here.
func TestCollectPinReachabilitySHAs_ReportsWhichFileFailed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	good := filepath.Join(dir, "good.yml")
	if err := os.WriteFile(good, []byte("jobs:\n  one:\n    uses: example/ci@1111111111111111111111111111111111111111\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("jobs:\n  - [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := collectPinReachabilitySHAs([]string{good, bad}, "example/ci")
	if err == nil {
		t.Fatal("a malformed second file was not reported")
	}

	if !strings.Contains(err.Error(), "bad.yml") {
		t.Errorf("err = %v, want it to name bad.yml so the operator knows which file to fix", err)
	}
}

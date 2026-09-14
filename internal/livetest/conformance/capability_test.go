// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package conformance_test

// PAR-CAP-1 carries no build tag on purpose.
//
// `Capabilities()` is derived from role membership, so a matrix that claims a
// feature the gates refuse is already impossible. What is still possible — and
// what happened — is the published table falling *behind* the model: the model
// carried a field the table did not list, so a reader could not discover from
// the docs that run artifacts work on Forgejo and not GitLab. This test closes
// that gap and keeps it closed, and it needs no forge to do it, so it runs on
// every commit.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// capabilityRows maps a published row label to the field it reports. The
// mapping is spelled out rather than derived: a row label is prose written for
// an adopter, and deciding which model field it stands for is exactly the
// editorial judgement a test should pin rather than guess.
func capabilityRows() map[string]string {
	return map[string]string{
		"SARIF / Code Scanning upload":                         "SARIFUpload",
		"SLSA build-provenance attestation API":                "Attestation",
		"Release asset upload":                                 "ReleaseAssets",
		"Run artifact upload/download":                         "RunArtifacts",
		"Keyless OIDC signing (public Fulcio, no extra flags)": "PublicFulcioTrusted",
		"Keyless OIDC signing against your own Fulcio":         "MintsOIDCToken",
		"Container tag deletion (package/registry API)":        "ContainerTagDeletion",
		"Container tag listing (package/registry API)":         "ContainerPackageListing",
	}
}

func TestCapabilityMatrix_MatchesTheAdapters(t *testing.T) {
	t.Parallel()

	table := readCapabilityTable(t)
	rows := capabilityRows()
	model := reflect.TypeFor[provider.Capabilities]()

	mapped := make(map[string]bool, len(rows))
	for label, name := range rows {
		field, ok := model.FieldByName(name)
		if !ok || !field.IsExported() || field.Type.Kind() != reflect.Bool || field.Tag.Get("json") == "-" {
			t.Fatalf("row %q maps to missing or non-capability field %q", label, name)
		}

		if mapped[name] {
			t.Fatalf("model field %s is mapped more than once", name)
		}

		mapped[name] = true
	}

	for _, field := range reflect.VisibleFields(model) {
		if field.IsExported() && field.Tag.Get("json") != "-" && !mapped[field.Name] {
			t.Errorf("model field %s has no editorial capability row", field.Name)
		}
	}

	// Every modelled capability must be published. This is the direction that
	// actually failed: the model grew a field and the table did not.
	if len(table) != len(rows) {
		t.Errorf("docs/providers.md publishes %d capability rows, the model has %d — every capability must be documented",
			len(table), len(rows))
	}

	for label, field := range rows {
		published, ok := table[label]
		if !ok {
			t.Errorf("capability %q is modelled but missing from the docs/providers.md matrix", label)

			continue
		}

		for _, forge := range livetest.Platforms() {
			capabilities, known := livetest.Capabilities(forge)
			if !known {
				t.Fatalf("no adapter for platform %q", forge)
			}

			want := reflect.ValueOf(capabilities).FieldByName(field).Bool()

			got, listed := published[string(forge)]
			if !listed {
				t.Errorf("capability %q does not list a value for %s", label, forge)

				continue
			}

			if got != want {
				t.Errorf("capability %q for %s: docs say %v, the adapter reports %v",
					label, forge, got, want)
			}
		}
	}
}

// readCapabilityTable parses the published matrix into label → forge → claim.
func readCapabilityTable(t *testing.T) map[string]map[string]bool {
	t.Helper()

	path := filepath.Join("..", "..", "..", "docs", "providers.md")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	platforms := make([]string, 0, len(livetest.Platforms()))
	for _, forge := range livetest.Platforms() {
		platforms = append(platforms, string(forge))
	}

	table, err := parseCapabilityTable(string(content), platforms)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	return table
}

// parseCapabilityTable reads the one table under the "## Capability matrix"
// heading. Its columns must be exactly the platforms, each once; its row labels
// must be unique; every cell must be a claim (✅) or its absence (❌). Anything
// else in that section, including a second table, is refused rather than merged
// in, and tables elsewhere in the document are never read.
func parseCapabilityTable(content string, platforms []string) (map[string]map[string]bool, error) {
	var (
		inSection, inTable, tableDone bool
		header                        []string
		table                         = map[string]map[string]bool{}
	)

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}

			inSection = line == "## Capability matrix"

			continue
		}

		if !inSection {
			continue
		}

		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			if inTable {
				inTable, tableDone = false, true
			}

			continue
		}

		cells := splitRow(line[1 : len(line)-1])

		switch {
		case tableDone:
			return nil, errCapabilityTable("a second table follows the capability matrix: %q", line)
		case header == nil:
			if cells[0] != "Capability" || !slices.Equal(sortedCopy(cells[1:]), sortedCopy(platforms)) || hasDuplicate(cells[1:]) {
				return nil, errCapabilityTable("capability header %q must name each of %v exactly once", line, platforms)
			}

			header, inTable = cells, true
		case strings.HasPrefix(cells[0], "---") || strings.HasPrefix(cells[0], ":-"):
			continue
		default:
			if err := addCapabilityRow(table, header, cells); err != nil {
				return nil, err
			}
		}
	}

	if len(table) == 0 {
		return nil, errCapabilityTable("found no capability matrix under \"## Capability matrix\"")
	}

	return table, nil
}

func addCapabilityRow(table map[string]map[string]bool, header, cells []string) error {
	if len(cells) != len(header) {
		return errCapabilityTable("capability row %q has %d cells, the header %d", cells[0], len(cells), len(header))
	}

	if _, seen := table[cells[0]]; seen {
		return errCapabilityTable("capability row %q is published twice", cells[0])
	}

	claims := make(map[string]bool, len(cells)-1)

	for i := 1; i < len(cells); i++ {
		switch cells[i] {
		case "✅":
			claims[header[i]] = true
		case "❌":
			claims[header[i]] = false
		default:
			return errCapabilityTable("capability row %q has an unreadable cell %q under %q", cells[0], cells[i], header[i])
		}
	}

	table[cells[0]] = claims

	return nil
}

func splitRow(line string) []string {
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))

	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}

	return cells
}

func errCapabilityTable(format string, args ...any) error {
	return fmt.Errorf(format, args...) //nolint:err113 // test-local parse diagnostics, compared by message.
}

func sortedCopy(values []string) []string {
	return slices.Sorted(slices.Values(values))
}

func hasDuplicate(values []string) bool {
	return len(slices.Compact(sortedCopy(values))) != len(values)
}

func TestParseCapabilityTable_ReadsOnlyTheScopedUniqueMatrix(t *testing.T) {
	t.Parallel()

	platforms := []string{"github", "local"}
	matrix := "## Capability matrix\n\nprose\n\n| Capability | github | local |\n|---|:---:|:---:|\n| A | ✅ | ❌ |\n| B | ❌ | ❌ |\n\nmore prose\n"

	got, err := parseCapabilityTable("| Capability | github | local |\n| Z | ✅ | ✅ |\n"+matrix+"## Other\n\n| Capability | github | local |\n| Y | ✅ | ✅ |\n", platforms)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]map[string]bool{"A": {"github": true, "local": false}, "B": {"github": false, "local": false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("table = %v, want %v", got, want)
	}

	for name, tc := range map[string]struct{ content, want string }{
		"duplicate row":         {strings.Replace(matrix, "| B |", "| A |", 1), `row "A" is published twice`},
		"duplicate column":      {strings.Replace(matrix, "| local |\n", "| github |\n", 1), "exactly once"},
		"missing column":        {strings.Replace(strings.Replace(matrix, " local |\n|---|:---:|:---:|", "\n|---|:---:|", 1), " ❌ |\n", "\n", 2), "exactly once"},
		"extra column":          {strings.Replace(matrix, "| local |\n", "| local | gitea |\n", 1), "exactly once"},
		"short row":             {strings.Replace(matrix, "| B | ❌ | ❌ |", "| B | ❌ |", 1), `row "B" has 2 cells`},
		"unreadable cell":       {strings.Replace(matrix, "| B | ❌ |", "| B | ? |", 1), "unreadable cell"},
		"second table":          {matrix + "\n| Capability | github | local |\n| C | ✅ | ✅ |\n", "second table"},
		"no matrix section":     {strings.Replace(matrix, "## Capability matrix", "## Capabilities", 1), "found no capability matrix"},
		"header not capability": {strings.Replace(matrix, "| Capability |", "| Feature |", 1), "exactly once"},
	} {
		if _, err := parseCapabilityTable(tc.content, platforms); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

// The SARIF summary labels are written per platform, deliberately: they are
// prose for an adopter, and a generic sentence derived from a bool would read
// worse than "Forgejo has no Code Scanning ingestion". What must not happen is
// the prose and the model disagreeing — a forge that gains SARIF ingestion while
// the summary still tells the reader its findings only become an artifact.
//
// So the wording stays hand-written and this pins it to the capability, the same
// bargain the matrix rows above strike.
func TestOpengrepSummary_AgreesWithTheSARIFCapability(t *testing.T) {
	t.Parallel()

	for _, forge := range livetest.Platforms() {
		capabilities, known := livetest.Capabilities(forge)
		if !known {
			t.Fatalf("no adapter for platform %q", forge)
		}

		for _, withToken := range []bool{true, false} {
			label := security.OpengrepCodeScanningLabel(security.OpengrepPlatformContext{
				Platform:             forge,
				HasCodeScanningToken: withToken,
			})

			// "upload" is claimed only where the forge can actually ingest.
			claimsUpload := strings.Contains(strings.ToLower(label), "upload")
			if claimsUpload && !capabilities.SARIFUpload {
				t.Errorf("%s: summary says %q but the adapter reports no SARIF ingestion", forge, label)
			}

			if !claimsUpload && capabilities.SARIFUpload {
				t.Errorf("%s: adapter ingests SARIF but the summary never mentions upload: %q", forge, label)
			}
		}
	}
}

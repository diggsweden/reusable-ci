// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

//nolint:gochecknoglobals // default scan roots — fixed paths read-only.
var defaultContractResidueRoots = []string{
	".github/workflows",
	"internal",
	"docs",
	"examples",
	"scripts",
	"containers",
	"README.md",
}

//nolint:gochecknoglobals // contract rule set — precompiled regex table.
var contractResidueRules = []contractResidueRule{
	{
		name:    "legacy parse-artifacts output",
		pattern: regexp.MustCompile(`\b(?:maven-artifacts|gradleandroid-artifacts|githubpackages-artifacts|goartifactfirst-artifacts|gocontainerfirst-artifacts|pipeline-sboms|any-require-authorization)\b`),
		message: "parse-artifacts emits only config-plan-json in v3",
	},
	{
		name:    "legacy underscored output alias",
		pattern: regexp.MustCompile(`\balready_published\b`),
		message: "use the canonical already-published output name",
	},
	{
		name:    "legacy uppercase metadata output",
		pattern: regexp.MustCompile(`\boutputs(?:\.|\[['"])(?:NAME|VERSION|GROUP_ID|ARTIFACT_ID|IS_SNAPSHOT)(?:\b|['"]\])|\.Set\(ctx,\s*"(?:NAME|VERSION|GROUP_ID|ARTIFACT_ID|IS_SNAPSHOT)"`),
		message: "use lowercase hyphenated metadata output names",
	},
	{
		name:    "legacy stage-result command",
		pattern: regexp.MustCompile(`\breusable-ci summary (?:build-stage-result|publish-stage-result|prepare-stage-result|pr-quality-stage-result|dev-publish-stage-result)\b`),
		message: "use reusable-ci summary stage-result with explicit target=result pairs",
	},
	{
		name:    "legacy internal workflow env alias",
		pattern: regexp.MustCompile(`\b(?:WORKDIR|WORKING_DIR|BINARY_NAME_INPUT|VERSION_INPUT|IMAGE_NAME_INPUT|ARTEFACT_NAME|TARGET_REGISTRY|UPLOAD_RESULT_JSON|RELEASE_VERSION_NO_V):`),
		message: "use the v3 canonical workflow env name",
	},
	{
		name:    "workflow digest marker shell",
		pattern: regexp.MustCompile(`\$\{DIGEST#sha256:|mkdir -p /tmp/digests|touch \"/tmp/digests`),
		message: "use reusable-ci container write-digest-marker",
	},
	{
		name:    "workflow direct go sbom shell",
		pattern: regexp.MustCompile(`run:\s*go mod download|cyclonedx-gomod mod -json`),
		message: "use reusable-ci build go download/sbom",
	},
	{
		name:    "stale metadata-action tag comment",
		pattern: regexp.MustCompile(`tags from metadata-action`),
		message: "metadata tags are produced by reusable-ci container metadata",
	},
}

type contractResidueRule struct {
	name    string
	pattern *regexp.Regexp
	message string
}

// ContractResidueInput drives ContractResidue.
type ContractResidueInput struct {
	// Root is the repository root. Empty -> cwd.
	Root string
	// Paths overrides the default source/workflow/doc roots to scan.
	Paths []string
	// FS overrides filesystem access for tests. When nil, the real OS filesystem
	// rooted at Root is used.
	FS fs.FS
}

// ContractResidue rejects removed compatibility contracts that must not return in
// the breaking v3 line.
func ContractResidue(w io.Writer, annot output.Annotator, in ContractResidueInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	root := in.Root
	if root == "" {
		var err error

		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	paths := in.Paths
	if len(paths) == 0 {
		paths = defaultContractResidueRoots
	}

	for i := range paths {
		paths[i] = filepath.ToSlash(filepath.Clean(paths[i]))
	}

	f := in.FS //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if f == nil {
		f = os.DirFS(root)
	}

	files, err := contractResidueFiles(f, paths)
	if err != nil {
		return err
	}

	failures := 0

	for _, file := range files {
		count, err := scanContractResidueFile(annot, f, file)
		if err != nil {
			return err
		}

		failures += count
	}

	if failures > 0 {
		return fmt.Errorf("v3 contract validation failed: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "V3 contracts look valid.")

	return nil
}

//nolint:cyclop // lists per-component contract files for each workflow this binary backs.
func contractResidueFiles(fsys fs.FS, roots []string) ([]string, error) {
	files := make([]string, 0)
	seen := make(map[string]struct{})

	for _, root := range roots {
		root = strings.TrimPrefix(root, "./")
		if root == "" {
			root = "."
		}

		info, err := fs.Stat(fsys, root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("stat %s: %w", root, err)
		}

		if !info.IsDir() {
			if shouldScanContractResidueFile(root) {
				files = appendContractResidueFile(files, seen, root)
			}

			continue
		}

		if err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				return err
			}

			if d.IsDir() {
				if skipContractResidueDir(d.Name()) {
					return fs.SkipDir
				}

				return nil
			}

			if shouldScanContractResidueFile(p) {
				files = appendContractResidueFile(files, seen, p)
			}

			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}

	sort.Strings(files)

	return files, nil
}

func appendContractResidueFile(files []string, seen map[string]struct{}, file string) []string {
	file = filepath.ToSlash(filepath.Clean(file))
	if _, ok := seen[file]; ok {
		return files
	}

	seen[file] = struct{}{}

	return append(files, file)
}

func skipContractResidueDir(name string) bool {
	switch name {
	case ".git", ".github-shared", "bin", "coverage", "dist", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func shouldScanContractResidueFile(file string) bool {
	if path.Base(file) == "coverage.html" || strings.HasSuffix(file, ".spdx.json") {
		return false
	}

	switch path.Ext(file) {
	case ".go", ".yml", ".yaml", ".md", ".sh", ".json", ".toml", ".txt":
		return true
	default:
		return false
	}
}

func scanContractResidueFile(annot output.Annotator, fsys fs.FS, file string) (int, error) {
	if file == "internal/app/validate/contractresidue.go" || file == "internal/app/validate/contractresidue_test.go" {
		return 0, nil
	}

	handle, err := fsys.Open(file)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", file, err)
	}

	defer func() { _ = handle.Close() }()

	scanner := bufio.NewScanner(handle)
	lineNo := 0
	failures := 0

	for scanner.Scan() {
		lineNo++

		line := scanner.Text()
		for _, rule := range contractResidueRules {
			if !rule.pattern.MatchString(line) {
				continue
			}

			// rule.name/rule.message are trusted static text; the Annotator
			// escapes the file property and the scanned-line message data.
			annot.ErrorAt(output.Annotation{File: file, Line: lineNo},
				"%s: %s (%s)", rule.name, rule.message, strings.TrimSpace(line))

			failures++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan %s: %w", file, err)
	}

	return failures, nil
}

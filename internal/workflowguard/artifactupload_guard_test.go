// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestUploadArtifactPathsAreNarrow scans every repository workflow and rejects
// upload-artifact steps whose path includes a forbidden workspace-wide glob.
func TestUploadArtifactPathsAreNarrow(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var (
		violations []string
		audited    int
	)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // workflow files under repo root.
		require.NoErrorf(t, readErr, "read %s", entry.Name())

		audited++

		violations = append(violations, auditUploadArtifactPaths(t, entry.Name(), body)...)
	}

	// Without this, an empty or wrongly filtered directory reports no
	// violations and looks identical to a clean one.
	require.GreaterOrEqual(t, audited, 20, "audited %d workflows; the walk, not the tree, is what was measured", audited)

	require.Emptyf(t, violations, "upload-artifact path-glob violations:\n  %s", strings.Join(violations, "\n  "))
}

// TestUploadArtifactPathGuardCatchesForbiddenEntries locks the scalar and
// multiline semantics independently of the repository's current workflows.
func TestUploadArtifactPathGuardCatchesForbiddenEntries(t *testing.T) {
	t.Parallel()

	const fixture = `name: fixture
on: push
jobs:
  scalar:
    steps:
      - name: Scalar
        uses: actions/upload-artifact@pinned
        with:
          path: .
  multiline:
    steps:
      - name: Multiline
        uses: actions/upload-artifact@pinned
        with:
          path: |
            dist/*.jar
            ./**/*
  workspace-expression:
    steps:
      - name: Workspace expression
        uses: actions/upload-artifact@pinned
        with:
          path: |
            ${{ github.workspace }}
            ${{ github.workspace }}/**/*
            ${{ github.workspace }}/dist/**
  workspace-env:
    steps:
      - name: Workspace env
        uses: actions/upload-artifact@pinned
        with:
          path: |
            $GITHUB_WORKSPACE
            $GITHUB_WORKSPACE/**
            $GITHUB_WORKSPACE/dist/**
  narrow:
    steps:
      - uses: actions/upload-artifact@pinned
        with:
          path: dist/**
`

	violations := auditUploadArtifactPaths(t, "fixture.yml", []byte(fixture))
	require.Len(t, violations, 6)
	report := strings.Join(violations, "\n")
	require.Contains(t, report, `path="."`)
	require.Contains(t, report, `path="./**/*"`)
	require.Contains(t, report, `path="${{ github.workspace }}"`)
	require.Contains(t, report, `path="${{ github.workspace }}/**/*"`)
	require.Contains(t, report, `path="$GITHUB_WORKSPACE"`)
	require.Contains(t, report, `path="$GITHUB_WORKSPACE/**"`)
	require.NotContains(t, report, `path="${{ github.workspace }}/dist/**"`)
	require.NotContains(t, report, `path="$GITHUB_WORKSPACE/dist/**"`)
}

func TestUploadArtifactPathGuardNormalizesAndFailsClosed(t *testing.T) {
	t.Parallel()

	for _, candidate := range []string{
		"dist/..",
		"dist/../",
		"../secrets",
		"${{ github.workspace }}/dist/..",
		"${{ inputs.artifact-path }}",
		"${{ github.workspace }}/${{ inputs.artifact-path }}",
		"dist/[[:alnum:]]*.jar",
		"dist/{debug,release}/*.apk",
		"dist/@(debug|release)/*.apk",
		"dist/+(release)/*.apk",
		"!dist/[0-9]*.tmp",
		"!!**/*",
		"!!dist/**",
		"!!!dist/**",
		"/tmp/output",
		`C:\runner\work`,
		"/var/**",
		"/home/**",
	} {
		if !isWorkspaceWideUploadPath(candidate) {
			t.Errorf("unsafe or indeterminate path %q was accepted", candidate)
		}
	}

	for _, candidate := range []string{
		"dist/**",
		"./dist/../release/**",
		"${{ github.workspace }}/dist/**",
		"$GITHUB_WORKSPACE/dist/**",
		"${{ inputs['working-directory'] }}/**/bom.json",
		"${{ inputs['working-directory'] }}/${{ inputs['build-module'] }}/build/**/*.apk",
		"**/dist",
		"!**/*.tmp",
		"!dist/**",
	} {
		if isWorkspaceWideUploadPath(candidate) {
			t.Errorf("scoped path %q was rejected", candidate)
		}
	}
}

func auditUploadArtifactPaths(t *testing.T, fileName string, body []byte) []string {
	t.Helper()

	var doc uploadWorkflow
	require.NoErrorf(t, yaml.Unmarshal(body, &doc), "parse %s", fileName)

	confined := confinedUploadRoots(doc)

	var violations []string

	for jobName, job := range doc.Jobs {
		for stepIdx, step := range job.Steps {
			action, _, _ := strings.Cut(step.Uses, "@")
			if !strings.EqualFold(action, "actions/upload-artifact") {
				continue
			}

			for _, path := range uploadPathLines(step.With.Path) {
				// Fixed outputs produced in these jobs' temporary filesystems.
				// Do not generalize this exception to arbitrary absolute trees.
				if path == "/tmp/digests/*" && (fileName == "publish-container.yml" || fileName == "self-runtime-container.yml") || path == "/tmp/nl-step-summary.md" && fileName == "lint-nanolinter.yml" {
					continue
				}

				if isWorkspaceWideUploadPath(path) {
					violations = append(violations, fmt.Sprintf(
						`%s: job %s, step %d (%s): path=%q is over-broad`,
						fileName, jobName, stepIdx, step.Name, path,
					))

					continue
				}

				if root, dynamic := leadingUploadExpression(path); dynamic && !confined[root] {
					violations = append(violations, fmt.Sprintf(
						`%s: job %s, step %d (%s): path=%q is rooted at %s, which nothing in this workflow proves stays inside the workspace`,
						fileName, jobName, stepIdx, step.Name, path, root,
					))
				}
			}
		}
	}

	return violations
}

// confinedUploadRoots returns the expressions this workflow proves confined.
// A dynamic upload root is a caller-supplied value: accepting it because it has
// a static suffix only proves the leaf, never the root it hangs off, so the
// workflow has to refuse an escaping value itself before anything consumes it.
func confinedUploadRoots(doc uploadWorkflow) map[string]bool {
	confined := map[string]bool{}

	for _, job := range doc.Jobs {
		for _, step := range job.Steps {
			refuses := true
			// A guard step earns its input the right to root an upload by
			// refusing both ways out of the workspace. A step that only says it
			// checks something proves nothing.
			for _, refusal := range []string{"must be workspace-relative", "must not contain parent-directory steps"} {
				refuses = refuses && strings.Contains(step.Run, refusal)
			}

			if !refuses {
				continue
			}

			for _, value := range step.Env {
				confined[strings.TrimSpace(value)] = true
			}
		}
	}

	return confined
}

// isRunOwnedRoot reports whether an expression names a context the runner itself
// defines. A caller cannot point those anywhere, so they root an upload without
// a per-workflow proof.
func isRunOwnedRoot(expression string) bool {
	switch strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expression, "${{"), "}}")) {
	case "github.workspace", "runner.temp":
		return true
	default:
		return false
	}
}

// leadingUploadExpression returns the expression a path is rooted at, when the
// first segment is one and the runner does not own it. A dynamic segment
// anywhere later hangs off a static scope and cannot move the root.
func leadingUploadExpression(candidate string) (string, bool) {
	candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "!"))
	if !strings.HasPrefix(candidate, "${{") {
		return "", false
	}

	end := strings.Index(candidate, "}}")
	if end < 0 {
		return "", false
	}

	expression := candidate[:end+2]
	if isRunOwnedRoot(expression) {
		return "", false
	}

	return expression, true
}

func isWorkspaceWideUploadPath(candidate string) bool {
	candidate = strings.TrimSpace(strings.ReplaceAll(candidate, `\`, "/"))
	if candidate == "" {
		return false
	}

	if strings.HasPrefix(candidate, "!") {
		candidate = strings.TrimPrefix(candidate, "!")
		if candidate == "" || strings.HasPrefix(candidate, "!") {
			return true
		}
	}

	relative, known := uploadPathRelativeToWorkspace(candidate)
	if !known {
		return true
	}

	if hasUnsupportedUploadGlobSyntax(relative) {
		return true
	}

	cleaned := path.Clean(relative)
	if cleaned == "." || cleaned == "/" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return true
	}

	segments := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for _, segment := range segments {
		if strings.Trim(segment, "*?") != "" {
			return false
		}
	}

	return true
}

func hasUnsupportedUploadGlobSyntax(candidate string) bool {
	// The guard models only doublestar's *, **, and ? forms. Character
	// classes, brace expansion, and extglobs have different parsers across
	// shells and upload-artifact versions, so they are indeterminate here.
	return strings.ContainsAny(candidate, "[]{}()")
}

// uploadPathRelativeToWorkspace resolves only expression structure that can be
// scoped statically. Unknown expressions, variables, and home paths are rejected
// by the caller instead of being guessed safe; fixed narrow absolute paths remain
// reviewable and are allowed.
func uploadPathRelativeToWorkspace(candidate string) (string, bool) {
	for _, root := range []string{"$GITHUB_WORKSPACE", "${GITHUB_WORKSPACE}"} {
		if suffix, ok := strings.CutPrefix(candidate, root); ok {
			if suffix != "" && !strings.HasPrefix(suffix, "/") {
				return "", false
			}

			return strings.TrimPrefix(suffix, "/"), !strings.ContainsAny(suffix, "${}")
		}
	}

	if strings.HasPrefix(candidate, "${{") {
		return normalizeExpressionUploadPath(candidate)
	}

	if strings.ContainsAny(candidate, "${}~") {
		return "", false
	}

	if path.IsAbs(candidate) || len(candidate) > 1 && candidate[1] == ':' {
		return "", false
	}

	return candidate, true
}

func normalizeExpressionUploadPath(candidate string) (string, bool) {
	const dynamicSegment = "__dynamic__"

	var normalized strings.Builder

	rest := candidate
	for rest != "" {
		start := strings.Index(rest, "${{")
		if start < 0 {
			normalized.WriteString(rest)

			break
		}

		normalized.WriteString(rest[:start])

		end := strings.Index(rest[start+3:], "}}")
		if end < 0 {
			return "", false
		}

		end += start + 3

		normalized.WriteString(dynamicSegment)

		rest = rest[end+2:]
	}

	cleaned := path.Clean(normalized.String())
	if cleaned == dynamicSegment || cleaned == "/"+dynamicSegment {
		return "", false
	}

	hasStaticScope := false

	for _, segment := range strings.Split(strings.TrimPrefix(cleaned, "/"), "/") {
		if segment == dynamicSegment || segment == "." || segment == ".." {
			continue
		}

		if strings.Trim(segment, "*?") != "" {
			hasStaticScope = true
		}
	}

	if !hasStaticScope || strings.ContainsAny(cleaned, "${}") {
		return "", false
	}

	return cleaned, true
}

func uploadPathLines(raw string) []string {
	var paths []string

	for _, line := range strings.Split(raw, "\n") {
		if path := strings.TrimSpace(line); path != "" {
			paths = append(paths, path)
		}
	}

	return paths
}

type uploadWorkflow struct {
	Jobs map[string]uploadJob `yaml:"jobs"`
}

type uploadJob struct {
	Steps []uploadStep `yaml:"steps"`
}

type uploadStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With uploadStepWith    `yaml:"with"`
}

type uploadStepWith struct {
	Path string `yaml:"path"`
}

// TestUploadArtifactDynamicRootsNeedProof covers the half a static suffix never
// established. `${{ inputs['x'] }}/dist/**` scopes the leaf and says nothing
// about where the root lands: a caller passing an absolute path or a parent
// step uploads from outside the workspace and the suffix still looks narrow.
// A workflow earns that root by refusing both escapes itself; runner-owned
// contexts need no proof because a caller cannot move them.
func TestUploadArtifactDynamicRootsNeedProof(t *testing.T) {
	t.Parallel()

	const guardStep = `      - name: Guard working-directory
        env:
          WORKING_DIRECTORY: ${{ inputs['working-directory'] }}
        run: |
          echo "working-directory must be workspace-relative"
          echo "working-directory must not contain parent-directory steps"
`

	upload := func(path string) string {
		return `jobs:
  build:
    steps:
` + path
	}
	uploadStep := `      - name: Upload
        uses: actions/upload-artifact@v7
        with:
          path: ${{ inputs['working-directory'] }}/dist/**
`

	for _, testCase := range []struct {
		name    string
		body    string
		want    int
		message string
	}{
		{name: "unproven caller root", body: upload(uploadStep), want: 1},
		{name: "root proved by the workflow", body: upload(guardStep + uploadStep), want: 0},
		{
			name: "a guard that only claims to check proves nothing",
			body: upload(`      - name: Guard working-directory
        env:
          WORKING_DIRECTORY: ${{ inputs['working-directory'] }}
        run: echo "checking working-directory"
` + uploadStep),
			want: 1,
		},
		{
			name: "a guard over a different value does not cover this root",
			body: upload(`      - name: Guard elsewhere
        env:
          OTHER: ${{ inputs['other-directory'] }}
        run: |
          echo "must be workspace-relative"
          echo "must not contain parent-directory steps"
` + uploadStep),
			want: 1,
		},
		{
			name: "runner-owned roots need no proof",
			body: upload(`      - name: Upload
        uses: actions/upload-artifact@v7
        with:
          path: ${{ github.workspace }}/dist/**
`),
			want: 0,
		},
		{
			// The existing narrowness rule only models expressions from the
			// start of the path, so this one is already refused as unscoped.
			// It is recorded here so the two rules stay distinguishable.
			name: "a static root with a later expression is refused as unscoped",
			body: upload(`      - name: Upload
        uses: actions/upload-artifact@v7
        with:
          path: dist/${{ inputs['build-module'] }}/*.jar
`),
			want: 1, message: "is over-broad",
		},
		{
			name: "a proven root with a further dynamic segment stays scoped",
			body: upload(guardStep + `      - name: Upload
        uses: actions/upload-artifact@v7
        with:
          path: ${{ inputs['working-directory'] }}/${{ inputs['build-module'] }}/build/outputs/*.aab
`),
			want: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			violations := auditUploadArtifactPaths(t, "fixture.yml", []byte(testCase.body))
			require.Len(t, violations, testCase.want, violations)

			if testCase.want > 0 {
				want := testCase.message
				if want == "" {
					want = "nothing in this workflow proves"
				}

				require.Contains(t, violations[0], want)
			}
		})
	}
}

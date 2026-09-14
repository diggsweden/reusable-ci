// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"gopkg.in/yaml.v3"
)

func TestWorkflowFilesUseYMLExtension(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob(filepath.Join(reporoot.Path(t), ".github", "workflows", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if len(matches) != 0 {
		t.Errorf("workflow files must use the repository's .yml convention; rename: %s", strings.Join(matches, ", "))
	}
}

// TestWorkflowInputContract proves that every `with:` key passed from
// one repo-internal workflow to another exists in the called workflow's
// `inputs:` declaration. This is the static-analysis-style guard that
// would have caught (for example) `release-publish-stage.yml` passing
// `sign-image: ...` to `publish-container.yml` before the input was
// declared, or a rename of a reusable workflow's input silently
// breaking every caller until the next production release.
//
// Scope: only internal calls (`uses: ./.github/workflows/X.yml`).
// External `uses:` (third-party actions, slsa-* refs) are out of
// scope because the test can't reach their input declarations
// without network — and `actionlint` already handles many of them.
func TestWorkflowInputContract(t *testing.T) {
	t.Parallel()

	documents := map[string][]byte{}

	for _, entry := range reporoot.ReadDir(t, ".github/workflows") {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yml") {
			documents[entry.Name()] = reporoot.ReadFile(t, ".github/workflows/"+entry.Name())
		}
	}

	require.NotEmpty(t, documents)
	failures, err := internalContractViolations(documents)
	require.NoError(t, err)
	require.Empty(t, failures)
}

func TestMegaLinterImageFailsClosedUnlessDigestPinned(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	workflowPath := filepath.Join(root, ".github", "workflows", "lint-megalinter.yml")

	body, readErr := os.ReadFile(workflowPath) //nolint:gosec // repository fixture.
	if readErr != nil {
		t.Fatal(readErr)
	}

	text := string(body)

	const requiredInput = `      megalinter-image:
        description: "Required trusted MegaLinter image reference pinned by @sha256:<64 lowercase hex>"
        required: true
        type: string`
	if !strings.Contains(text, requiredInput) {
		t.Fatal("lint-megalinter must require the caller to provide a trusted image")
	}

	if strings.Contains(text, "oxsecurity/megalinter:v8") {
		t.Fatal("lint-megalinter must not retain a mutable MegaLinter default")
	}

	var workflow struct {
		Jobs map[string]struct {
			Container any `yaml:"container"`
			Steps     []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}

	job := workflow.Jobs["megalinter-lint"]
	if job.Container != nil {
		t.Fatal("caller-selected MegaLinter image must not start as a job container before validation")
	}

	steps := job.Steps
	validationAt, checkoutAt, runAt := -1, -1, -1
	validationScript := ""

	for idx, step := range steps {
		switch step.Name {
		case "Validate trusted MegaLinter image digest":
			validationAt = idx
			validationScript = step.Run
		case "Checkout repository":
			checkoutAt = idx
		case "Run MegaLinter (security gate)":
			runAt = idx
		}
	}

	if validationAt < 0 || checkoutAt <= validationAt || runAt <= checkoutAt {
		t.Fatalf("MegaLinter boundary order validation=%d checkout=%d run=%d", validationAt, checkoutAt, runAt)
	}

	// The pattern is lifted out of the workflow's own script rather than
	// copied here, so the cases below test what actually runs. Evaluating it in
	// Go instead of shelling out to bash keeps this in the ordinary offline
	// suite: no subprocess, no bash on PATH, and a case table that can afford
	// to be exhaustive because each case costs nothing.
	pattern := megalinterImagePattern(t, validationScript)

	for _, tc := range []struct {
		name  string
		image string
		valid bool
		why   string
	}{
		{
			name:  "digest-pinned",
			image: "registry.example.com/security/megalinter:8@sha256:" + strings.Repeat("a", 64),
			valid: true,
			why:   "a reviewed image, pinned to the bytes that were reviewed",
		},
		{
			name:  "digest with no tag",
			image: "registry.example.com/security/megalinter@sha256:" + strings.Repeat("a", 64),
			valid: true,
			why:   "the tag is decoration once a digest is present",
		},
		{name: "mutable tag", image: "oxsecurity/megalinter:v8", why: "the whole point: v8 can be republished"},
		{name: "bare name", image: "oxsecurity/megalinter", why: "resolves to :latest"},
		{name: "empty", image: "", why: "an unset input must not pass"},
		{name: "short digest", image: "registry.example.com/megalinter@sha256:abc", why: "abbreviated digests are ambiguous"},
		{
			name:  "uppercase digest",
			image: "registry.example.com/megalinter@sha256:" + strings.Repeat("A", 64),
			why:   "hex digests are lowercase; accepting both spellings would let two strings name one image",
		},
		{
			name:  "digest with trailing content",
			image: "registry.example.com/megalinter@sha256:" + strings.Repeat("a", 64) + " --privileged",
			why:   "the value reaches a docker run argv; anything after the digest is an injected argument",
		},
		{
			name:  "digest with a leading dash",
			image: "-registry.example.com/megalinter@sha256:" + strings.Repeat("a", 64),
			why:   "a leading dash would be read by docker as a flag",
		},
		{
			name:  "newline before the digest",
			image: "oxsecurity/megalinter:v8\nregistry.example.com/megalinter@sha256:" + strings.Repeat("a", 64),
			why:   "an unanchored check would match the second line and start the first image",
		},
		{
			name:  "a different algorithm",
			image: "registry.example.com/megalinter@sha512:" + strings.Repeat("a", 64),
			why:   "the workflow pins sha256; another algorithm is not the reviewed reference",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := pattern.MatchString(tc.image); got != tc.valid {
				t.Errorf("accepted=%v, want %v for %q: %s", got, tc.valid, tc.image, tc.why)
			}
		})
	}

	for _, relative := range []string{
		".github/workflows/pullrequest-orchestrator.yml",
		".github/workflows/pullrequest-quality-stage.yml",
		"templates/megalinter.yml",
	} {
		content, readErr := os.ReadFile(filepath.Join(root, relative)) //nolint:gosec // repository fixture.
		if readErr != nil {
			t.Fatal(readErr)
		}

		if strings.Contains(string(content), "oxsecurity/megalinter:v8") {
			t.Errorf("%s retains a mutable MegaLinter default", relative)
		}
	}

	template, err := os.ReadFile(filepath.Join(root, "templates", "megalinter.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(template), "regex: '^[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[0-9a-f]{64}$'") {
		t.Fatal("GitLab MegaLinter input must reject non-digest image references during configuration")
	}
}

func TestPublishContainerAlwaysRemovesMaterializedBuildSecrets(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", "publish-container.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	script, err := materializedCleanup(body)
	if err != nil {
		t.Fatal(err)
	}

	checkMaterializedCleanup(t, script)
}

func TestPinnedCompanionSuiteUsesStrictBlackboxContract(t *testing.T) {
	t.Parallel()

	const (
		pin                 = "b9f7ab2a22c2fae30ec8df5194c64ff094ff0843"
		validatorInvocation = "run: ./scripts/validate-strict-profile-contract.sh"
	)

	for _, workflow := range []string{"self-pullrequest.yml", "self-release.yml"} {
		body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", workflow)) //nolint:gosec // repository fixture.
		if err != nil {
			t.Fatal(err)
		}

		text := string(body)
		for _, want := range []string{
			"ref: " + pin,
			"name: Validate strict companion behavioral contract",
			validatorInvocation,
			"REUSABLE_CI_BLACKBOX_PROFILE: strict",
			"run: go test -tags=blackbox -count=1 -timeout 45m ./tests/blackbox/...",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s pinned companion contract is missing %q", workflow, want)
			}
		}

		if got := strings.Count(text, pin); got != 1 {
			t.Errorf("%s immutable companion pin occurrences = %d, want checkout only", workflow, got)
		}

		checkoutAt := strings.Index(text, "ref: "+pin)
		validationAt := strings.Index(text, validatorInvocation)
		setupAt := -1

		if checkoutAt >= 0 {
			if relative := strings.Index(text[checkoutAt:], "- name: Set up Go"); relative >= 0 {
				setupAt = checkoutAt + relative
			}
		}

		runAt := strings.Index(text, "run: go test -tags=blackbox")
		if checkoutAt < 0 || setupAt <= checkoutAt || validationAt <= setupAt || runAt <= validationAt {
			t.Errorf("%s companion gate order checkout=%d setup=%d validation=%d run=%d", workflow, checkoutAt, setupAt, validationAt, runAt)
		}
	}
}

func TestPromoteWorkflowResolvesDryRunAsBoolean(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", "promote-stage.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	text := string(body)
	if strings.Contains(text, `${DRY_RUN:+--dry-run}`) {
		t.Fatal("non-empty string expansion would add --dry-run when DRY_RUN=false")
	}

	if !strings.Contains(text, `[[ "$DRY_RUN" == true ]] && args+=(--dry-run)`) {
		t.Fatal("promote must add --dry-run only for the resolved true boolean")
	}

	if got := strings.Count(text, `[[ "$DRY_RUN" == true ]] && exit 0`); got != 1 {
		t.Fatalf("cleanup dry-run guards = %d, want 1", got)
	}

	if strings.Contains(text, "container ledger rollback") {
		t.Fatal("promote-stage must retry forward rather than delete tags without a promotion journal")
	}
}

func TestProductionReleaseCeremonyUsesRequestThenFinalTag(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)

	// Walked rather than globbed at a fixed depth: examples/ is grouped
	// (examples/signing/…, examples/gitlab/…), and an example that quietly
	// dropped out of this check by moving one level down would be the exact
	// failure this guard exists to prevent. A new example is picked up with no
	// counter to bump.
	var examples []string

	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !d.IsDir() && d.Name() == "release-workflow.yml" {
			examples = append(examples, path)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(examples) == 0 {
		t.Fatal("no examples/**/release-workflow.yml found; the ceremony check would pass vacuously")
	}

	paths := make([]string, 1, 1+len(examples))
	paths[0] = filepath.Join(root, ".github", "workflows", "self-release.yml")

	paths = append(paths, examples...)
	for _, path := range paths {
		body, readErr := os.ReadFile(path) //nolint:gosec // repository fixture.
		if readErr != nil {
			t.Fatal(readErr)
		}

		if !requestTagTrigger(body, "release-request/v*") {
			t.Errorf("%s does not trigger on release-request/v*", path)
		}
	}

	orchestrator, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release-orchestrator.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	text := string(orchestrator)
	derive := strings.Index(text, "run: reusable-ci version derive-release")

	requireRequest := strings.Index(text, `RELEASE_CONTEXT_REQUIRE_REQUEST: "true"`)
	if derive < 0 || requireRequest < 0 || requireRequest > derive {
		t.Fatal("orchestrator must require a request ref at its first derive-release boundary")
	}

	if got := strings.Count(text, `branch: ${{ needs.execute-prepare-stage.outputs.release-sha }}`); got != 2 {
		t.Fatalf("post-tag build/publish immutable-SHA checkouts = %d, want 2", got)
	}

	if !strings.Contains(text, `checkout-ref: ${{ needs.execute-prepare-stage.outputs.release-sha }}`) {
		t.Fatal("release creation must check out the validated immutable release SHA")
	}

	runtimeWorkflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "self-runtime-container.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(runtimeWorkflow), `- "v*.*.*"`) {
		t.Fatal("self-runtime-container must remain triggered by final release tags")
	}

	runtimeText := string(runtimeWorkflow)
	if !strings.Contains(runtimeText, "sha256sum --check --strict checksums.txt") ||
		strings.Count(runtimeText, "release publish-self-runtime-cli") != 1 {
		t.Fatal("self-runtime CLI publication must checksum-bootstrap this run's CLI and invoke the typed private publisher exactly once")
	}

	for _, requiredPolicy := range []string{
		"group: self-runtime-cli-v3.0.0-pre",
		"cancel-in-progress: false",
		"github.event_name == 'workflow_dispatch' && inputs.publish && github.ref_type != 'tag'",
		`'$2 == name { count++ } END { print count + 0 }'`,
		`if [[ "$checksum_entries" != 1 ]]`,
	} {
		if !strings.Contains(runtimeText, requiredPolicy) {
			t.Errorf("self-runtime CLI publication is missing policy %q", requiredPolicy)
		}
	}

	if strings.Count(runtimeText, "concurrency:") != 1 {
		t.Error("only the destructive publish-cli job may be serialized")
	}

	for _, removedShellPolicy := range []string{"/releases/tags/${tag}", "jq -r '.upload_url'", "--data-binary"} {
		if strings.Contains(runtimeText, removedShellPolicy) {
			t.Errorf("self-runtime CLI publication still contains raw release API policy %q", removedShellPolicy)
		}
	}
}

func TestReleasePreparationSerializesBumpsAndTagsOnce(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)

	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release-prepare-stage.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	text := string(body)
	require.Empty(t, preparationViolations(body))

	if got := strings.Count(text, "run: reusable-ci version bump-plan"); got != 1 {
		t.Fatalf("serialized complete-plan bump steps = %d, want exactly 1", got)
	}

	if got := strings.Count(text, "run: reusable-ci version commit-push"); got != 1 {
		t.Fatalf("release preparation commit steps = %d, want exactly 1", got)
	}

	if got := strings.Count(text, "run: reusable-ci version tag-release"); got != 1 {
		t.Fatalf("final tag creation steps = %d, want exactly 1", got)
	}

	bumpAt := strings.Index(text, "run: reusable-ci version bump-plan")
	commitAt := strings.Index(text, "run: reusable-ci version commit-push")

	tagAt := strings.Index(text, "run: reusable-ci version tag-release")
	if bumpAt < 0 || commitAt <= bumpAt || tagAt <= commitAt {
		t.Fatalf("release preparation order bump=%d commit=%d tag=%d", bumpAt, commitAt, tagAt)
	}

	if !strings.Contains(text, "AUTHORIZED_SOURCE_SHA: ${{ inputs['authorized-source-sha'] }}") {
		t.Fatal("release preparation must lease-check the authorized source SHA before mutation")
	}

	for _, relative := range []string{"docs/artifacts-reference.md", "examples/monorepo/README.md"} {
		doc, readErr := os.ReadFile(filepath.Join(root, relative)) //nolint:gosec // repository fixture.
		if readErr != nil {
			t.Fatal(readErr)
		}

		if strings.Contains(string(doc), "can race on pushes/tag moves") {
			t.Errorf("%s still claims multi-artifact release refs race", relative)
		}
	}
}

func TestReleaseRecoveryAndIdentityContracts(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	readWorkflow := func(name string) string {
		t.Helper()

		body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", name)) //nolint:gosec // repository fixture.
		if err != nil {
			t.Fatal(err)
		}

		return string(body)
	}

	orchestrator := readWorkflow("release-orchestrator.yml")
	for _, want := range []string{
		`group: reusable-release-${{ github.repository }}-${{ github.ref }}`,
		`authorized-source-sha: ${{ github.sha }}`,
		`existing-release-sha: ${{ needs.validate-prerequisites.outputs.existing-release-sha }}`,
	} {
		if !strings.Contains(orchestrator, want) {
			t.Errorf("release orchestrator is missing %q", want)
		}
	}

	if strings.Contains(orchestrator, `group: release-${{ github.repository }}-${{ inputs.branch }}`) {
		t.Fatal("release orchestrator still uses branch-wide concurrency")
	}

	prepare := readWorkflow("release-prepare-stage.yml")
	for _, want := range []string{
		`ref: ${{ inputs['existing-release-sha'] || inputs['authorized-source-sha'] }}`,
		`if: ${{ inputs['existing-release-sha'] != '' }}`,
		`run: reusable-ci validate tag commit --require-head`,
		`if: ${{ inputs['existing-release-sha'] == '' && fromJson(inputs['prepare-stage-plan-json']).targets.version_bump.runs }}`,
	} {
		if !strings.Contains(prepare, want) {
			t.Errorf("release preparation recovery contract is missing %q", want)
		}
	}

	selfRelease := readWorkflow("self-release.yml")
	if !strings.Contains(selfRelease, `group: self-release-${{ github.repository }}-${{ github.ref }}`) {
		t.Fatal("self release must use a request-scoped concurrency group distinct from the reusable workflow")
	}

	publish := readWorkflow("publish-container.yml")
	for _, forbidden := range []string{
		`GITHUB_REF_NAME: ${{ github.ref_name }}`,
		`REF_NAME: ${{ github.ref_name }}`,
		`--tag "$REF_NAME"`,
	} {
		if strings.Contains(publish, forbidden) {
			t.Errorf("container release boundary still uses ambient ref identity %q", forbidden)
		}
	}

	for _, want := range []string{
		`GITHUB_REF_NAME: ${{ inputs['release-tag'] || needs.prep.outputs.source-ref-name }}`,
		`RELEASE_TAG: ${{ inputs['release-tag'] }}`,
		`--tag "$RELEASE_TAG"`,
	} {
		if !strings.Contains(publish, want) {
			t.Errorf("container release boundary is missing explicit identity %q", want)
		}
	}
}

func TestRuntimeContainerfileExternalFromImagesAreDigestPinned(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), "containers", "runtime", "Containerfile")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	for _, violation := range unpinnedRuntimeStages(string(body)) {
		t.Error(violation)
	}
}

func unpinnedRuntimeStages(body string) []string {
	var violations []string

	args := make(map[string]string)
	stages := make(map[string]bool)
	argPattern := regexp.MustCompile(`(?i)^ARG[ \t]+([A-Za-z_][A-Za-z0-9_]*)=(\S+)$`)
	variablePattern := regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	digestPattern := regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)

	for lineNumber, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if match := argPattern.FindStringSubmatch(line); match != nil {
			args[match[1]] = match[2]

			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}

		imageIndex := 1
		if len(fields) > imageIndex && strings.HasPrefix(fields[imageIndex], "--platform=") {
			imageIndex++
		}

		if imageIndex >= len(fields) {
			violations = append(violations, fmt.Sprintf("Containerfile:%d missing FROM image", lineNumber+1))

			continue
		}

		image := variablePattern.ReplaceAllStringFunc(fields[imageIndex], func(variable string) string {
			name := strings.Trim(variable, "${}")

			return args[name]
		})
		if image != "scratch" && !stages[image] && !digestPattern.MatchString(image) {
			violations = append(violations, fmt.Sprintf("Containerfile:%d external FROM %q is not SHA-256 digest-pinned", lineNumber+1, image))
		}

		if len(fields) > imageIndex+2 && strings.EqualFold(fields[imageIndex+1], "AS") {
			stages[fields[imageIndex+2]] = true
		}
	}

	return violations
}

// megalinterImagePattern extracts the ERE the workflow validates with and
// compiles it, so a change to the workflow changes what these cases test.
// Bash's [[ =~ ]] and Go's regexp agree on this pattern's constructs; the
// anchors are in the pattern itself, which is what makes the newline case
// above meaningful.
func megalinterImagePattern(t *testing.T, script string) *regexp.Regexp {
	t.Helper()

	match := regexp.MustCompile(`=~\s+(\S+)\s*\]\]`).FindStringSubmatch(script)
	if match == nil {
		t.Fatalf("no [[ =~ ]] image check found in the validation step:\n%s", script)
	}

	pattern, err := regexp.Compile(match[1])
	if err != nil {
		t.Fatalf("compile %q: %v", match[1], err)
	}

	return pattern
}

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package conformance_test

// The probe builders and the runtime-refusal judgement below carry no build tag
// on purpose, like jsonShape. The live scenarios send them to a forge, and the
// fixtures here execute them offline; behind the live tag the fixtures ran only
// inside an authorized lab run, so no ordinary test run checked them.

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

type probeStep struct {
	ID  string            `yaml:"id"`
	Run string            `yaml:"run"`
	Env map[string]string `yaml:"env"`
}

type probeJob struct {
	Stage     string      `yaml:"stage"`
	Script    []string    `yaml:"script"`
	Steps     []probeStep `yaml:"steps"`
	Artifacts struct {
		Reports map[string]string `yaml:"reports"`
	} `yaml:"artifacts"`
	Needs []struct {
		Job       string `yaml:"job"`
		Artifacts bool   `yaml:"artifacts"`
	} `yaml:"needs"`
}

type probeFixture struct {
	Stages  []string            `yaml:"stages"`
	Detect  probeJob            `yaml:"detect"`
	Consume probeJob            `yaml:"consume"`
	Jobs    map[string]probeJob `yaml:"jobs"`
}

func decodeProbeFixture(t *testing.T, body string) probeFixture {
	t.Helper()

	var fixture probeFixture
	if err := yaml.Unmarshal([]byte(body), &fixture); err != nil {
		t.Fatal(err)
	}

	return fixture
}

func runProbeShell(t *testing.T, script string, env ...string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-eu", "-c", script) //nolint:gosec // bounded, reviewed fixture script; closed environment and owned working directory.
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "LC_ALL=C"}, env...)

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("fixture exceeded its deadline: %s", out)
	}

	return err
}

func TestProbeFixtures_AnnotationRequiresExecutedReport(t *testing.T) {
	t.Parallel()

	const prelude = `PROBE_READY=true
run_product() {
  test "$PROBE_READY" = true
  test "$1" = doctor
  printf '%s\n' "$FIXTURE_REPORT"
  return "$FIXTURE_STATUS"
}`
	for _, forge := range []provider.ForgeAPI{provider.ForgeForgejo, provider.ForgeGitLab} {
		fixture := decodeProbeFixture(t, annotationProbe(forge, prelude))

		script := strings.Join(fixture.Detect.Script, "\n")
		if forge == provider.ForgeForgejo {
			script = fixture.Jobs["detect"].Steps[0].Run
		}

		for _, tc := range []struct {
			name, report, status string
			valid                bool
		}{
			{"healthy", "artifacts.yml present", "0", true},
			{"validation", "artifacts.yml present", "1", true},
			{"empty", "", "0", false},
			{"missing", "command not found", "127", false},
			{"parse", "artifacts.yml present", "64", false},
			{"annotation", "artifacts.yml present\n::error::wrong dialect", "1", false},
		} {
			t.Run(string(forge)+"/"+tc.name, func(t *testing.T) {
				err := runProbeShell(t, script, "FIXTURE_REPORT="+tc.report, "FIXTURE_STATUS="+tc.status)
				if (err == nil) != tc.valid {
					t.Errorf("fixture exit=%v, valid=%t", err, tc.valid)
				}
			})
		}
	}
}

func TestProbeFixtures_OutputsHaveNativeConsumers(t *testing.T) {
	t.Parallel()

	gitlab := decodeProbeFixture(t, outputFileProbe(provider.ForgeGitLab, "fixture_prelude"))
	if !reflect.DeepEqual(gitlab.Stages, []string{"produce", "verify"}) || gitlab.Detect.Stage != "produce" || gitlab.Consume.Stage != "verify" {
		t.Fatal("producer must precede consumer")
	}

	if gitlab.Detect.Artifacts.Reports["dotenv"] != "build.env" || len(gitlab.Consume.Needs) != 1 || gitlab.Consume.Needs[0].Job != "detect" || !gitlab.Consume.Needs[0].Artifacts {
		t.Fatal("consumer is not wired to the producer's dotenv artifact")
	}

	forgejo := decodeProbeFixture(t, outputFileProbe(provider.ForgeForgejo, "fixture_prelude"))

	steps := forgejo.Jobs["detect"].Steps
	if len(steps) != 2 || steps[0].ID != "metadata" {
		t.Fatal("a separately identified producer step is required")
	}

	wantEnv := map[string]string{"VERSION": "${{ steps.metadata.outputs.version }}", "VERSION_NO_V": "${{ steps.metadata.outputs['version-no-v'] }}", "PROJECT_NAME": "${{ steps.metadata.outputs['project-name'] }}"}
	if !reflect.DeepEqual(steps[1].Env, wantEnv) {
		t.Fatalf("consumer bindings = %v", steps[1].Env)
	}

	for name, script := range map[string]string{"gitlab": strings.Join(gitlab.Consume.Script, "\n"), "forgejo": steps[1].Run} {
		t.Run(name, func(t *testing.T) {
			values := []string{"VERSION=v9.9.9-parrun5", "VERSION_NO_V=9.9.9-parrun5", "PROJECT_NAME=outputs"}
			if err := runProbeShell(t, script, values...); err != nil {
				t.Fatalf("valid consumer values refused: %v", err)
			}

			for index, value := range values {
				bad := append([]string(nil), values...)
				key, _, _ := strings.Cut(value, "=")

				bad[index] = key + "=wrong"
				if err := runProbeShell(t, script, bad...); err == nil {
					t.Errorf("consumer accepted wrong %s", key)
				}
			}
		})
	}
}

type refusalTB struct {
	livetest.TB
	failed bool
}

func (*refusalTB) Helper()                  {}
func (tb *refusalTB) Errorf(string, ...any) { tb.failed = true }

func TestProbeFixtures_ArtifactRefusalRejectsUnrelatedOutcomes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		code    int
		message string
		valid   bool
	}{
		{int(errs.ExitCodeUsage), "FORGEJO_RUN_ID/GITHUB_RUN_ID is required", true},
		{int(errs.ExitCodeUsage), "cannot upload outside a CI job", true},
		{0, "", false},
		{int(errs.ExitCodeUsage), "unknown flag", false},
		{int(errs.ExitCodeUnavailable), "cannot upload outside a CI job", false},
		{int(errs.ExitCodeConfiguration), "unsupported on this platform", false},
	} {
		recorder := &refusalTB{}
		assertArtifactRuntimeRefusal(recorder, provider.ForgeForgejo, livetest.Run{ExitCode: tc.code, Stderr: tc.message})

		if recorder.failed == tc.valid {
			t.Errorf("code=%d message=%q: failed=%t", tc.code, tc.message, recorder.failed)
		}
	}
}

// annotationProbe drives a verb that reports through the annotator and fails the
// job if GitHub's dialect appears.
//
// `doctor` must produce a real report, with its documented success or validation
// status. A missing binary or a parser failure is not annotation evidence.
func annotationProbe(forge provider.ForgeAPI, prelude string) string {
	check := prelude + `
doctor_status=0
run_product doctor > out.txt 2>&1 || doctor_status=$?
cat out.txt
test "$doctor_status" -eq 0 || test "$doctor_status" -eq 1
grep -q 'artifacts.yml present' out.txt

if grep -qE '::(error|warning|notice|group|endgroup)::' out.txt; then
  echo "FAIL: GitHub workflow commands emitted on a runner that does not render them"
  exit 1
fi`

	if forge == provider.ForgeGitLab {
		return `detect:
  image: ` + livetest.ProbeImage + `
  script:
    - |
      ` + strings.ReplaceAll(check, "\n", "\n      ") + `
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: the annotation dialect must match the runner
        run: |
          ` + strings.ReplaceAll(check, "\n", "\n          ") + `
`
}

// indent re-indents a multi-line shell block so it survives being spliced into
// YAML, where a stray column changes meaning.
func indent(block string, spaces int) string {
	pad := strings.Repeat(" ", spaces)

	return strings.ReplaceAll(block, "\n", "\n"+pad)
}

// outputFileProbe checks both the written file and the value a later consumer
// receives through the runner's native output mechanism.
//
// The Forgejo half also settles which variable wins. The runner sets both names,
// so the probe compares the two paths rather than assuming: when they resolve to
// the same file the question is moot and it says so, and when they differ the
// native $FORGEJO_OUTPUT is required to be the one written. Asserting a
// preference that the runner's own configuration makes unobservable would be
// testing the fixture.
func outputFileProbe(forge provider.ForgeAPI, prelude string) string {
	const resolve = `run_product release resolve metadata \
  --version v9.9.9-parrun5 --repository livetest/outputs`

	if forge == provider.ForgeGitLab {
		// GitLab does not provide an output file; the pipeline nominates one,
		// which is the documented contract rather than a fixture convenience.
		return `stages: [produce, verify]
detect:
  stage: produce
  image: ` + livetest.ProbeImage + `
  variables:
    CI_OUTPUT: build.env
  script:
    - |
      ` + indent(prelude, 6) + `
      ` + indent(resolve, 6) + `

      echo "--- $CI_OUTPUT ---"
      cat "$CI_OUTPUT"

      # Upper snake case, because that is what GitLab dotenv accepts and what a
      # later job will reference as $VERSION_NO_V.
      grep -q '^VERSION=v9.9.9-parrun5$' "$CI_OUTPUT"
      grep -q '^VERSION_NO_V=9.9.9-parrun5$' "$CI_OUTPUT"
      grep -q '^PROJECT_NAME=outputs$' "$CI_OUTPUT"
  artifacts:
    reports:
      dotenv: build.env
consume:
  stage: verify
  image: ` + livetest.ProbeImage + `
  needs:
    - job: detect
      artifacts: true
  script:
    - test "$VERSION" = "v9.9.9-parrun5"
    - test "$VERSION_NO_V" = "9.9.9-parrun5"
    - test "$PROJECT_NAME" = "outputs"
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: outputs must land in the file this runner reads
        id: metadata
        run: |
          ` + indent(prelude, 10) + `

          echo "FORGEJO_OUTPUT=${FORGEJO_OUTPUT:-unset}"
          echo "GITHUB_OUTPUT=${GITHUB_OUTPUT:-unset}"

          if [ -z "${FORGEJO_OUTPUT:-}" ] && [ -z "${GITHUB_OUTPUT:-}" ]; then
            echo "FAIL: this runner provided no step-output file under either name"
            exit 1
          fi

          ` + indent(resolve, 10) + `

          # The native name is preferred; the alias is the fallback. Which file to
          # read back is therefore the same decision the product just made.
          target="${FORGEJO_OUTPUT:-$GITHUB_OUTPUT}"
          echo "--- $target ---"
          cat "$target"

          grep -q '^version=v9.9.9-parrun5$' "$target"
          grep -q '^version-no-v=9.9.9-parrun5$' "$target"
          grep -q '^project-name=outputs$' "$target"

          # When the runner points both names at one file the preference is
          # unobservable, and claiming to have proven it would be a lie.
          if [ -n "${FORGEJO_OUTPUT:-}" ] && [ -n "${GITHUB_OUTPUT:-}" ]; then
            if [ "$FORGEJO_OUTPUT" = "$GITHUB_OUTPUT" ]; then
              echo "NOTE: both names point at one file; native-vs-alias preference is not observable here"
            elif grep -q '^version=v9.9.9-parrun5$' "$GITHUB_OUTPUT"; then
              echo "FAIL: the value went to the \$GITHUB_OUTPUT alias while \$FORGEJO_OUTPUT is set"
              exit 1
            fi
          fi
      - name: a later step consumes the runner outputs
        env:
          VERSION: ${{ steps.metadata.outputs.version }}
          VERSION_NO_V: ${{ steps.metadata.outputs['version-no-v'] }}
          PROJECT_NAME: ${{ steps.metadata.outputs['project-name'] }}
        run: |
          test "$VERSION" = "v9.9.9-parrun5"
          test "$VERSION_NO_V" = "9.9.9-parrun5"
          test "$PROJECT_NAME" = "outputs"
`
}

func assertArtifactRuntimeRefusal(tb livetest.TB, forge provider.ForgeAPI, run livetest.Run) {
	tb.Helper()

	lower := strings.ToLower(run.Stderr)

	runtimeMessage := strings.Contains(lower, "outside a ci job") ||
		(forge == provider.ForgeForgejo && strings.Contains(lower, "forgejo_run_id/github_run_id is required"))
	if run.ExitCode != int(errs.ExitCodeUsage) || !runtimeMessage {
		tb.Errorf("%s: want an outside-runtime usage refusal, got exit %d\nstderr: %s", forge, run.ExitCode, run.Stderr)
	}
}

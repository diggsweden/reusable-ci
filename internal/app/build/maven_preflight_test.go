// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type mavenPreflightOps struct {
	run  func(context.Context, io.Writer, io.Writer, []string) error
	eval func(context.Context, string) (string, error)
}

func (f mavenPreflightOps) RunInherit(ctx context.Context, out, stderr io.Writer, args ...string) error {
	return f.run(ctx, out, stderr, args)
}

func (f mavenPreflightOps) EvalExpression(ctx context.Context, expr string) (string, error) {
	return f.eval(ctx, expr)
}

type mavenPreflightSummary func(context.Context, string) error

func (f mavenPreflightSummary) Append(ctx context.Context, body string) error {
	return f(ctx, body)
}

func TestMavenLocalPreflight_RefusesBeforeEffects(t *testing.T) { //nolint:gocognit // the two public paths share the complete refusal-state oracle and owned fixtures.
	const (
		completePOM = `<project><version>${revision}</version><groupId>gov.fixture</groupId><artifactId>child</artifactId></project>`
		declaration = `<?xml version="1.0"?>`
		doctype     = `<!DOCTYPE project>`
	)

	for _, tc := range []struct {
		name, pom, message string
		want               error
	}{
		{"missing POM", "", "pom.xml not found", errs.ErrMissingInput},
		{"POM is directory", "", "read pom.xml", nil},
		{"empty POM", "", "parse pom.xml", errs.ErrInvalidConfig},
		{"malformed POM", `<project><version>1.2.3</project>`, "parse pom.xml", errs.ErrInvalidConfig},
		{"broken suffix", completePOM + `<broken`, "parse pom.xml", errs.ErrInvalidConfig},
		{"second root", completePOM + `<project/>`, "parse pom.xml", errs.ErrInvalidConfig},
		{"trailing text", completePOM + `unexpected`, "parse pom.xml", errs.ErrInvalidConfig},
		{"leading text", `unexpected` + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration after root", completePOM + declaration, "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration duplicate", declaration + declaration + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration inside root", strings.Replace(completePOM, "</project>", declaration+"</project>", 1), "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration after comment", `<!-- prolog -->` + declaration + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration after whitespace", " \t\n" + declaration + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration after PI", `<?prepare?>` + declaration + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"XML declaration wrong case", `<?XmL version="1.0"?>` + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"DOCTYPE after root", completePOM + doctype, "parse pom.xml", errs.ErrInvalidConfig},
		{"DOCTYPE inside root", strings.Replace(completePOM, "</project>", doctype+"</project>", 1), "parse pom.xml", errs.ErrInvalidConfig},
		{"DOCTYPE duplicate", doctype + doctype + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"bogus directive before root", `<!bogus>` + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"bogus directive after root", completePOM + `<!bogus>`, "parse pom.xml", errs.ErrInvalidConfig},
		{"DOCTYPE lookalike", `<!DOCTYPEsuffix project>` + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"DOCTYPE missing name", `<!DOCTYPE >` + completePOM, "parse pom.xml", errs.ErrInvalidConfig},
		{"missing version", `<project><groupId>gov.fixture</groupId><artifactId>child</artifactId></project>`, "project.version missing", errs.ErrMissingInput},
		{"missing group after expression", `<project><version>${revision}</version><artifactId>child</artifactId></project>`, "project.groupId missing", errs.ErrMissingInput},
		{"missing artifact after expressions", `<project><version>${revision}</version><groupId>${group}</groupId></project>`, "project.artifactId missing", errs.ErrMissingInput},
		{"blank version", `<project><version> &#x9; </version><groupId>gov.fixture</groupId><artifactId>child</artifactId></project>`, "project.version missing", errs.ErrMissingInput},
		{"blank group", `<project><version>${revision}</version><groupId> &#xA; </groupId><artifactId>child</artifactId></project>`, "project.groupId missing", errs.ErrMissingInput},
		{"blank artifact", `<project><version>${revision}</version><groupId>${group}</groupId><artifactId> </artifactId></project>`, "project.artifactId missing", errs.ErrMissingInput},
		{"parent missing version", `<project><parent><groupId>gov.parent</groupId><artifactId>parent</artifactId></parent><artifactId>child</artifactId></project>`, "project.version missing", errs.ErrMissingInput},
		{"parent missing group after expression", `<project><parent><version>${revision}</version><artifactId>parent</artifactId></parent><artifactId>child</artifactId></project>`, "project.groupId missing", errs.ErrMissingInput},
		{"parent artifact not inherited", `<project><parent><version>${revision}</version><groupId>${group}</groupId><artifactId>parent</artifactId></parent></project>`, "project.artifactId missing", errs.ErrMissingInput},
	} {
		for _, entry := range []string{"release", "metadata"} {
			t.Run(tc.name+"/"+entry, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				t.Chdir(fsys.MkdirAll("cwd"))
				fsys.WriteFile("cwd/pom.xml", []byte(`<project><version>9.0.0</version><groupId>cwd.decoy</groupId><artifactId>decoy</artifactId></project>`))
				fsys.WriteFile("cwd/native-selected.xml", []byte(`<project><modelVersion>4.0.0</modelVersion><version>${revision}</version><groupId>gov.native</groupId><artifactId>native-child</artifactId></project>`))
				dir := fsys.MkdirAll("selected metadata")

				switch tc.name {
				case "missing POM":
				case "POM is directory":
					fsys.MkdirAll("selected metadata/pom.xml")
				default:
					fsys.WriteFile("selected metadata/pom.xml", []byte(tc.pom))
				}

				fsys.WriteFile("selected metadata/target/prior.jar", []byte("preserve selected artifact"))

				canary := fsys.WriteFile("cwd/target/prior.jar", []byte("preserve native artifact"))
				if err := os.Chmod(canary, 0o400); err != nil {
					t.Fatal(err)
				}

				beforeTree := ownedTree(t, fsys.Root)
				in := appbuild.MavenReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
					BuildType:           "app",
					CLIOpts:             []string{"--batch-mode", "-f", "native-selected.xml", "-Drevision=4.5.6"},
					SBOMToolVersion:     "2.9.1",
				}
				beforeInput := in
				beforeInput.CLIOpts = slices.Clone(in.CLIOpts)

				var (
					runs  [][]string
					evals []string
				)

				ops := mavenPreflightOps{
					run: func(_ context.Context, _, _ io.Writer, args []string) error {
						runs = append(runs, slices.Clone(args))

						return nil
					},
					eval: func(_ context.Context, expr string) (string, error) {
						evals = append(evals, expr)

						return "native-value", nil
					},
				}
				stdout, stderr := bytes.NewBufferString("prior stdout\n"), bytes.NewBufferString("prior stderr\n")
				summary := bytes.NewBufferString("prior summary\n")
				summaryCalls := 0
				summarySink := mavenPreflightSummary(func(_ context.Context, body string) error {
					summaryCalls++
					_, err := summary.WriteString(body)

					return err
				})

				sink := fakeoutputsink.New(t)
				if err := sink.Set(t.Context(), "prior-output", "keep"); err != nil {
					t.Fatal(err)
				}

				beforeOutputs, beforeOrder := sink.AllScalar(), sink.Order()

				var err error
				if entry == "release" {
					err = appbuild.MavenReleaseBuild(t.Context(), summarySink, ops, stdout, stderr, in)
				} else {
					err = appbuild.MavenMetadata(t.Context(), sink, ops, stdout, appbuild.MavenMetadataInput{Dir: dir})
				}

				if err == nil || !strings.Contains(err.Error(), tc.message) || (tc.want != nil && !errors.Is(err, tc.want)) {
					t.Errorf("error=%v, want %q / %v", err, tc.message, tc.want)
				}

				if len(runs) != 0 || len(evals) != 0 {
					t.Errorf("local refusal called Maven: runs=%v evals=%v", runs, evals)
				}

				if summaryCalls != 0 || summary.String() != "prior summary\n" || stdout.String() != "prior stdout\n" || stderr.String() != "prior stderr\n" {
					t.Errorf("local refusal changed reporting: summaries=%d summary=%q stdout=%q stderr=%q", summaryCalls, summary, stdout, stderr)
				}

				if !maps.Equal(beforeOutputs, sink.AllScalar()) || !slices.Equal(beforeOrder, sink.Order()) || sink.CloseCount() != 0 {
					t.Error("local refusal changed output sink")
				}

				if !reflect.DeepEqual(beforeInput, in) || !maps.Equal(beforeTree, ownedTree(t, fsys.Root)) {
					t.Error("local refusal changed input or fixture paths, bytes, or modes")
				}
			})
		}
	}
}

func TestMavenReleaseBuild_LocalPreflightPreservesNativeOrder(t *testing.T) { //nolint:gocognit // literal, inherited and interpolated controls pin the same full native/reporting boundary.
	for _, tc := range []struct {
		name, pom, identity string
		evals               []string
	}{
		{"literal", `<project><version>1.2.3</version><groupId>gov.literal</groupId><artifactId>literal-child</artifactId></project>`, "gov.literal:literal-child:1.2.3", nil},
		{"inherited", `<project><parent><version>2.3.4</version><groupId>gov.parent</groupId><artifactId>not-child</artifactId></parent><version> </version><groupId> </groupId><artifactId>inherited-child</artifactId></project>`, "gov.parent:inherited-child:2.3.4", nil},
		{"interpolated", `<project><version>${revision}</version><groupId>${group}</groupId><artifactId>${artifact}</artifactId></project>`, "gov.cwd:cwd-child:8.7.6-SNAPSHOT", []string{"project.version", "project.groupId", "project.artifactId"}},
		{"inherited expression", `<project><parent><version>${revision}</version><groupId>gov.parent</groupId><artifactId>not-child</artifactId></parent><artifactId>expression-child</artifactId></project>`, "gov.parent:expression-child:8.7.6-SNAPSHOT", []string{"project.version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			cwd := fsys.MkdirAll("cwd")
			t.Chdir(cwd)
			fsys.WriteFile("cwd/pom.xml", []byte(`<project><modelVersion>4.0.0</modelVersion><version>8.7.6-SNAPSHOT</version><groupId>gov.cwd</groupId><artifactId>cwd-child</artifactId></project>`))
			fsys.WriteFile("cwd/native-selected.xml", []byte(`<project><modelVersion>4.0.0</modelVersion><version>${revision}</version><groupId>gov.native</groupId><artifactId>native-child</artifactId><profiles><profile><id>native-profile</id></profile></profiles></project>`))
			fsys.WriteFile("metadata/pom.xml", []byte(tc.pom))
			fsys.WriteFile("cwd/target/prior.jar", []byte("preserve native artifact"))
			before := ownedTree(t, fsys.Root)
			in := appbuild.MavenReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: "../metadata/.", SkipTests: true},
				BuildType:           "app",
				CLIOpts:             []string{"--batch-mode", "-f", "native-selected.xml", "-Drevision=4.5.6-SNAPSHOT", "-Pnative-profile"},
				JavaVersion:         "25",
			}
			beforeInput := in
			beforeInput.CLIOpts = slices.Clone(in.CLIOpts)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			stdout, stderr := bytes.NewBufferString("prior stdout\n"), bytes.NewBufferString("prior stderr\n")

			var (
				events, evals []string
				runs          [][]string
			)

			ops := mavenPreflightOps{
				run: func(gotCtx context.Context, out, errout io.Writer, args []string) error {
					if gotCtx != ctx || out != stdout || errout != stderr {
						t.Error("native run lost context or distinct writers")
					}

					gotCWD, err := os.Getwd()
					if err != nil || gotCWD != cwd {
						t.Errorf("native cwd=%q err=%v, want %q", gotCWD, err, cwd)
					}

					runs = append(runs, slices.Clone(args))
					events = append(events, "run "+args[len(in.CLIOpts)])
					_, _ = fmt.Fprintf(out, "native stdout %d\n", len(runs))
					_, _ = fmt.Fprintf(errout, "native stderr %d\n", len(runs))

					return nil
				},
				eval: func(gotCtx context.Context, expr string) (string, error) {
					if gotCtx != ctx || len(runs) != 1 || runs[0][len(in.CLIOpts)] != "install" {
						t.Error("expression evaluated without caller context and completed sibling install")
					}

					gotCWD, err := os.Getwd()
					if err != nil || gotCWD != cwd {
						t.Errorf("eval cwd=%q err=%v, want %q", gotCWD, err, cwd)
					}

					evals = append(evals, expr)
					events = append(events, "eval "+expr)

					// EvalExpression receives no CLIOpts: these are the CWD POM's
					// coordinates, not the -f/profile/property-selected build's.
					return map[string]string{"project.version": " 8.7.6-SNAPSHOT\n", "project.groupId": " gov.cwd\n", "project.artifactId": " cwd-child\n"}[expr], nil
				},
			}

			var summaries []string

			summary := mavenPreflightSummary(func(gotCtx context.Context, body string) error {
				if gotCtx != ctx {
					t.Error("summary lost caller context")
				}

				events = append(events, "summary")
				summaries = append(summaries, body)

				return nil
			})
			if err := appbuild.MavenReleaseBuild(ctx, summary, ops, stdout, stderr, in); err != nil {
				t.Fatal(err)
			}

			wantEvents := make([]string, 0, len(tc.evals)+4)

			wantEvents = append(wantEvents, "run install")
			for _, expr := range tc.evals {
				wantEvents = append(wantEvents, "eval "+expr)
			}

			wantEvents = append(wantEvents, "run clean", "summary", "summary")

			wantRuns := [][]string{
				{"--batch-mode", "-f", "native-selected.xml", "-Drevision=4.5.6-SNAPSHOT", "-Pnative-profile", "install", "-DskipTests"},
				{"--batch-mode", "-f", "native-selected.xml", "-Drevision=4.5.6-SNAPSHOT", "-Pnative-profile", "clean", "package", "-DskipTests"},
			}
			if !slices.Equal(events, wantEvents) || !slices.Equal(evals, tc.evals) || !reflect.DeepEqual(runs, wantRuns) {
				t.Errorf("events=%v evals=%v runs=%v", events, evals, runs)
			}

			wantOut := "prior stdout\nnative stdout 1\nMaven release build (app): " + tc.identity + "\nBuilding Maven application...\nnative stdout 2\n"
			if stdout.String() != wantOut || stderr.String() != "prior stderr\nnative stderr 1\nnative stderr 2\n" {
				t.Errorf("stdout=%q stderr=%q", stdout, stderr)
			}

			if len(summaries) != 2 || !strings.Contains(summaries[0], "Generation disabled") || !strings.Contains(summaries[1], "- **Artifact:** `"+tc.identity+"`\n") {
				t.Errorf("summaries=%q", summaries)
			}

			if !maps.Equal(before, ownedTree(t, fsys.Root)) || !reflect.DeepEqual(beforeInput, in) {
				t.Error("fake-only successful build changed fixture or input")
			}
		})
	}
}

func TestMavenReleaseBuild_LocalPreflightCapturesLiteralsButEvaluatesCurrentModel(t *testing.T) {
	for _, version := range []string{"3.4.5", "${revision}"} {
		t.Run(version, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Chdir(fsys.Root)
			fsys.WriteFile("pom.xml", []byte(`<project><parent><version>`+version+`</version><groupId>gov.captured</groupId><artifactId>parent</artifactId></parent><artifactId>captured-child</artifactId></project>`))

			var events []string

			ops := mavenPreflightOps{
				run: func(_ context.Context, _, _ io.Writer, args []string) error {
					events = append(events, "run "+args[0])
					if args[0] == "install" {
						fsys.WriteFile("pom.xml", []byte(`<project><version>9.9.9</version><groupId>gov.replaced</groupId><artifactId>different-child</artifactId></project>`))
					}

					return nil
				},
				eval: func(_ context.Context, expr string) (string, error) {
					events = append(events, "eval "+expr)

					// Native evaluation sees the replaced CWD model after install;
					// capturing the local POM does not freeze expression results.
					return map[string]string{"project.version": "9.9.9", "project.groupId": "gov.replaced", "project.artifactId": "different-child"}[expr], nil
				},
			}

			var stdout, stderr, summary bytes.Buffer

			sink := mavenPreflightSummary(func(_ context.Context, body string) error {
				_, err := summary.WriteString(body)

				return err
			})
			if err := appbuild.MavenReleaseBuild(t.Context(), sink, ops, &stdout, &stderr, appbuild.MavenReleaseBuildInput{BuildType: "app"}); err != nil {
				t.Fatal(err)
			}

			wantEvents := []string{"run install", "run clean"}
			wantVersion := "3.4.5"

			if version == "${revision}" {
				wantEvents = []string{"run install", "eval project.version", "run clean"}
				wantVersion = "9.9.9"
			}

			if !slices.Equal(events, wantEvents) || !strings.Contains(stdout.String(), "gov.captured:captured-child:"+wantVersion+"\n") ||
				!strings.Contains(summary.String(), "`gov.captured:captured-child:"+wantVersion+"`") || strings.Contains(stdout.String()+summary.String(), "gov.replaced") || stderr.Len() != 0 {
				t.Fatalf("events=%v stdout=%q stderr=%q summary=%q", events, &stdout, &stderr, &summary)
			}
		})
	}
}

func TestMavenReleaseBuild_LocalPreflightDefersNativeFailures(t *testing.T) {
	t.Parallel()

	installFailure := errors.New("owned install failure") //nolint:err113 // distinct native boundary sentinel.

	expansionFailure := errors.New("owned expansion failure") //nolint:err113 // distinct native boundary sentinel.
	for _, tc := range []struct {
		name, answer, message string
		runErr, evalErr, want error
		events                []string
	}{
		{"install", "4.5.6", "mvn install", installFailure, nil, installFailure, []string{"run install"}},
		{"expansion", "", "expand project.version", nil, expansionFailure, expansionFailure, []string{"run install", "eval project.version"}},
		{"empty expansion", " \n\t", "expand project.version returned an empty value", nil, nil, errs.ErrMalformedInput, []string{"run install", "eval project.version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("pom.xml", []byte(`<project><version>${revision}</version><groupId>gov.fixture</groupId><artifactId>child</artifactId></project>`))
			before := ownedTree(t, fsys.Root)

			var events []string

			ops := mavenPreflightOps{
				run: func(_ context.Context, _, _ io.Writer, args []string) error {
					events = append(events, "run "+args[0])

					return tc.runErr
				},
				eval: func(_ context.Context, expr string) (string, error) {
					events = append(events, "eval "+expr)

					return tc.answer, tc.evalErr
				},
			}
			summary := mavenPreflightSummary(func(context.Context, string) error {
				events = append(events, "summary")

				return nil
			})
			stdout, stderr := bytes.NewBufferString("prior stdout\n"), bytes.NewBufferString("prior stderr\n")

			err := appbuild.MavenReleaseBuild(t.Context(), summary, ops, stdout, stderr, appbuild.MavenReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Root},
				BuildType:           "app",
			})
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.message) || !slices.Equal(events, tc.events) {
				t.Errorf("err=%v events=%v, want %v / %v", err, events, tc.want, tc.events)
			}

			if stdout.String() != "prior stdout\n" || stderr.String() != "prior stderr\n" || !maps.Equal(before, ownedTree(t, fsys.Root)) {
				t.Error("native failure allowed later reporting or changed fake-only fixture")
			}
		})
	}
}

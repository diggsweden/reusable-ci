// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type npmPreflightRunner func(context.Context, string, io.Writer, io.Writer, []string) error

func (f npmPreflightRunner) RunInherit(ctx context.Context, dir string, out, stderr io.Writer, args ...string) error {
	return f(ctx, dir, out, stderr, args)
}

type npmPreflightSummary func(context.Context, string) error

func (f npmPreflightSummary) Append(ctx context.Context, body string) error {
	return f(ctx, body)
}

func TestNPMReleaseBuild_LocalPreflightRefusesBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body, script, scope, message string
	}{
		{name: "blank name", body: `{"name":" \t ","version":"1.0.0"}`, message: "package.json name is required"},
		{name: "blank name with scope", body: `{"name":" \t ","version":"1.0.0"}`, scope: "@org/", message: "package.json name is required"},
		{name: "unicode blank name", body: `{"name":"\u2003\u00a0","version":"1.0.0"}`, message: "package.json name is required"},
		{name: "blank version after scoped name", body: `{"name":"@org/app","version":" \r\n "}`, scope: "@org/", message: "package.json version is required"},
		{name: "unicode blank version", body: `{"name":"@org/app","version":"\u2003\u00a0"}`, scope: "@org", message: "package.json version is required"},
		{name: "missing name", body: `{"version":"1.0.0"}`, message: "package.json name is required"},
		{name: "null version", body: `{"name":"@org/app","version":null}`, message: "package.json version is required"},
		{name: "typed identity", body: `{"name":"@org/app","version":42}`, message: "parse package.json"},
		{name: "malformed JSON", body: `{"name":"@org/app","version":"1.0.0"`, message: "parse package.json"},
		{name: "numeric build", body: `{"name":"@org/app","version":"1.0.0","scripts":{"build":42}}`, message: "package.json"},
		{name: "boolean build", body: `{"name":"@org/app","version":"1.0.0","scripts":{"build":true}}`, message: "package.json"},
		{name: "object build", body: `{"name":"@org/app","version":"1.0.0","scripts":{"build":{"token":"npm-preflight-secret"}}}`, message: "package.json"},
		{name: "array build", body: `{"name":"@org/app","version":"1.0.0","scripts":{"build":["npm-preflight-secret"]}}`, message: "package.json"},
		{name: "scripts is string", body: `{"name":"@org/app","version":"1.0.0","scripts":"npm-preflight-secret"}`, message: "package.json"},
		{name: "scripts is array", body: `{"name":"@org/app","version":"1.0.0","scripts":[]}`, message: "package.json"},
		{name: "custom selected script", body: `{"name":"@org/app","version":"1.0.0","scripts":{"build":"tsc","npm-preflight-secret":42}}`, script: "npm-preflight-secret", message: "package.json"},
	} {
		for _, seeded := range []bool{false, true} {
			state := "fresh"
			if seeded {
				state = "seeded"
			}

			t.Run(tc.name+"/"+state, func(t *testing.T) {
				t.Parallel()
				fsys := testfs.NewReal(t)
				dir := fsys.MkdirAll("selected package")
				fsys.WriteFile("selected package/package.json", []byte(tc.body))
				fsys.WriteFile("package.json", []byte(`{"name":"@org/decoy","version":"9.9.9","scripts":{"build":"tsc"}}`))
				fsys.WriteFile("canary/prior.tgz", []byte("unrelated artifact"))
				fsys.WriteFile("selected package/.npmrc", []byte("//registry.invalid/:_authToken=npm-preflight-secret\n"))

				if seeded {
					fsys.WriteFile("selected package/node_modules/prior", []byte("installed dependency"))
					fsys.WriteFile("selected package/package-lock.json", []byte(`{"lockfileVersion":3}`))
					fsys.WriteFile("selected package/org-app-1.0.0.tgz", []byte("prior tarball"))
					bom := fsys.WriteFile("selected package/bom.json", []byte("prior SBOM"))
					require.NoError(t, os.Chmod(bom, 0o400))
				}

				before := ownedTree(t, fsys.Root)
				npmRunner, npxRunner := &fakeNPMRunner{}, &fakeNPMRunner{}
				stdout, stderr := bytes.NewBufferString("prior stdout\n"), bytes.NewBufferString("prior stderr\n")
				annotations := bytes.NewBufferString("prior annotations\n")
				summary := bytes.NewBufferString("prior summary\n")
				summaryCalls := 0
				sink := npmPreflightSummary(func(_ context.Context, body string) error {
					summaryCalls++
					_, err := summary.WriteString(body)

					return err
				})
				in := appbuild.NPMReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
					PackageScope:        tc.scope, ScriptName: tc.script, SBOMToolVersion: "4.2.1",
				}
				beforeInput := in
				err := appbuild.NPMReleaseBuild(t.Context(), sink, npmRunner, npxRunner, output.NewAnnotator(annotations, output.FormatGitHub), stdout, stderr, in)
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
				assert.Contains(t, err.Error(), tc.message)
				assert.NotContains(t, err.Error(), "npm-preflight-secret")
				assert.Empty(t, npmRunner.calls, "local refusal must precede ci, test, build and pack")
				assert.Empty(t, npxRunner.calls)
				assert.Zero(t, summaryCalls)
				assert.Equal(t, "prior summary\n", summary.String())
				assert.Equal(t, "prior stdout\n", stdout.String())
				assert.Equal(t, "prior stderr\n", stderr.String())
				assert.Equal(t, "prior annotations\n", annotations.String())
				assert.Equal(t, beforeInput, in)
				assert.Equal(t, before, ownedTree(t, fsys.Root))
			})
		}
	}
}

func TestNPMMetadata_LocalPreflightRejectsBlankIdentity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body, message string }{
		{"name", `{"name":" \t ","version":"1.2.3"}`, "package.json name is required"},
		{"version", `{"name":"@org/app","version":" \t "}`, "package.json version is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			fsys.WriteFile("package.json", []byte(tc.body))
			before := ownedTree(t, fsys.Root)
			sink := fakeoutputsink.New(t)
			require.NoError(t, sink.Set(t.Context(), "prior", "keep"))

			var out, annotations bytes.Buffer

			err := appbuild.NPMMetadata(t.Context(), sink, &out, output.NewAnnotator(&annotations, output.FormatGitHub), appbuild.NPMMetadataInput{Dir: fsys.Root})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			assert.Contains(t, err.Error(), tc.message)
			assert.Equal(t, map[string]string{"prior": "keep"}, sink.AllScalar())
			assert.Equal(t, []string{"prior"}, sink.Order())
			assert.Zero(t, sink.CloseCount())
			assert.Empty(t, out.String())
			assert.Empty(t, annotations.String())
			assert.Equal(t, before, ownedTree(t, fsys.Root))
		})
	}
}

func TestNPMMetadata_LocalPreflightRetainsLiteralIdentity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, version, scope string }{
		{"@org/app", "2.0.0-rc.1+build.007", "@org/"},
		{"app-name.with_underscore", "1.2.3", ""},
		{" @org/app ", " 1.2.3 ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			body, err := json.Marshal(map[string]string{"name": tc.name, "version": tc.version})
			require.NoError(t, err)
			// Metadata does not consume scripts; only release/application checks them.
			fsys.WriteFile("package.json", []byte(strings.TrimSuffix(string(body), "}")+`,"scripts":{"build":42}}`))
			before := ownedTree(t, fsys.Root)
			sink := fakeoutputsink.New(t)

			var out, annotations bytes.Buffer

			err = appbuild.NPMMetadata(t.Context(), sink, &out, output.NewAnnotator(&annotations, output.FormatGitHub), appbuild.NPMMetadataInput{Dir: fsys.Root, PackageScope: tc.scope})
			require.NoError(t, err)
			require.Equal(t, map[string]string{"name": tc.name, "version": tc.version}, sink.AllScalar())
			require.Equal(t, []string{"name", "version"}, sink.Order())
			require.Equal(t, "Package: "+tc.name+"@"+tc.version+"\n", out.String())
			require.Empty(t, annotations.String())
			require.Equal(t, before, ownedTree(t, fsys.Root))
		})
	}
}

func TestNPMReleaseBuild_LocalPreflightPreservesNativeSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, packageName, version, scope, scripts, script, postScripts, wantScript string
	}{
		{name: "scoped stable", packageName: "@org/app", version: "1.2.3", scope: "@org/", scripts: `,"scripts":{"build":"tsc"}`, wantScript: "build"},
		{name: "unscoped prerelease", packageName: "app-name", version: "2.0.0-rc.1", scripts: `,"scripts":{"build":"tsc"}`, wantScript: "build"},
		{name: "build metadata", packageName: "@org/app", version: "2.0.0+build.007", scope: "@org", scripts: `,"scripts":{"build":"tsc"}`, wantScript: "build"},
		{name: "custom script", packageName: "@org/app", version: "2.0.0-beta.2+build.7", scripts: `,"scripts":{"build":"tsc","bundle:release":"bundler"}`, script: "bundle:release", wantScript: "bundle:release"},
		{name: "no scripts", packageName: "@org/app", version: "1.0.0"},
		{name: "empty scripts", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{}`},
		{name: "null scripts", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":null`},
		{name: "absent build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"test":"tap"}`},
		{name: "empty build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"build":""}`},
		{name: "blank build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"build":" \t\n "}`},
		{name: "null build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"build":null}`},
		{name: "absent custom does not run build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"build":"tsc"}`, script: "bundle"},
		{name: "postinstall adds build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"postinstall":"generate-build"}`, postScripts: `,"scripts":{"build":"generated"}`, wantScript: "build"},
		{name: "postinstall enables custom", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"bundle":"","postinstall":"generate-build"}`, script: "bundle", postScripts: `,"scripts":{"bundle":"generated"}`, wantScript: "bundle"},
		{name: "postinstall removes build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"build":"tsc","postinstall":"remove-build"}`, postScripts: `,"scripts":{}`},
		{name: "postinstall repairs unused lint", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"build":"tsc","lint":42,"postinstall":"repair-lint"}`, postScripts: `,"scripts":{"build":"tsc","lint":"eslint","postinstall":"repair-lint"}`, wantScript: "build"},
		{name: "postinstall repairs unselected build", packageName: "@org/app", version: "1.0.0", scripts: `,"scripts":{"bundle":"bundler","build":42,"postinstall":"repair-build"}`, script: "bundle", postScripts: `,"scripts":{"bundle":"bundler","build":"tsc","postinstall":"repair-build"}`, wantScript: "bundle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			dir := fsys.MkdirAll("selected package")
			identity, err := json.Marshal(map[string]string{"name": tc.packageName, "version": tc.version})
			require.NoError(t, err)

			prefix := strings.TrimSuffix(string(identity), "}")
			fsys.WriteFile("selected package/package.json", []byte(prefix+tc.scripts+"}"))
			fsys.WriteFile("canary/prior.tgz", []byte("unrelated artifact"))
			canary := ownedTree(t, fsys.Path("canary"))

			var archive bytes.Buffer

			gz := gzip.NewWriter(&archive)
			tw := tar.NewWriter(gz)
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o600, Size: int64(len(identity))}))
			_, err = tw.Write(identity)
			require.NoError(t, err)
			require.NoError(t, tw.Close())
			require.NoError(t, gz.Close())

			filename := strings.ReplaceAll(strings.TrimPrefix(tc.packageName, "@"), "/", "-") + "-" + tc.version + ".tgz"

			var (
				calls                       [][]string
				events, summaries           []string
				stdout, stderr, annotations bytes.Buffer
			)

			npmRunner := npmPreflightRunner(func(ctx context.Context, gotDir string, out, errout io.Writer, args []string) error {
				require.Equal(t, t.Context(), ctx)
				require.Equal(t, dir, gotDir)
				require.Same(t, &stderr, errout)

				calls = append(calls, slices.Clone(args))

				events = append(events, "npm "+args[0])
				if args[0] != "pack" {
					require.Same(t, &stdout, out)
				}

				if args[0] == "ci" && tc.postScripts != "" {
					fsys.WriteFile("selected package/package.json", []byte(prefix+tc.postScripts+"}"))
				}

				if args[0] == "pack" {
					require.Len(t, args, 4)
					require.Equal(t, []string{"pack", "--json", "--pack-destination"}, args[:3])
					require.Equal(t, dir, filepath.Dir(args[3]))
					require.NoError(t, os.WriteFile(filepath.Join(args[3], filename), archive.Bytes(), 0o600))

					return json.NewEncoder(out).Encode([]map[string]string{{"name": tc.packageName, "version": tc.version, "filename": filename}})
				}

				return nil
			})
			npxRunner := &fakeNPMRunner{}
			summary := npmPreflightSummary(func(_ context.Context, body string) error {
				events = append(events, "summary")
				summaries = append(summaries, body)

				return nil
			})
			err = appbuild.NPMReleaseBuild(t.Context(), summary, npmRunner, npxRunner, output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, appbuild.NPMReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
				PackageScope:        tc.scope, ScriptName: tc.script, SBOMToolVersion: "4.2.1", NodeVersion: "24",
			})
			require.NoError(t, err)

			wantCalls := [][]string{{"ci"}, {"test"}}
			wantEvents := []string{"npm ci", "npm test"}

			if tc.wantScript != "" {
				wantCalls = append(wantCalls, []string{"run", tc.wantScript})
				wantEvents = append(wantEvents, "npm run")

				require.Contains(t, stdout.String(), "Running \""+tc.wantScript+"\" npm script")
			} else {
				require.Contains(t, stdout.String(), "skipping build step")
			}

			require.Len(t, calls, len(wantCalls)+1)
			wantCalls = append(wantCalls, []string{"pack", "--json", "--pack-destination", calls[len(calls)-1][3]})
			require.Equal(t, wantCalls, calls)
			require.Equal(t, append(wantEvents, "summary", "npm pack", "summary"), events)
			require.Len(t, npxRunner.calls, 1)
			require.Equal(t, []string{"--yes", "@cyclonedx/cyclonedx-npm@4.2.1", "--output-format", "json", "--output", npxRunner.calls[0][5]}, npxRunner.calls[0])
			require.Equal(t, []string{dir}, npxRunner.dirs)
			require.Equal(t, archive.Bytes(), fsys.ReadFile("selected package/"+filename))
			require.JSONEq(t, `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`, string(fsys.ReadFile("selected package/bom.json")))
			require.Len(t, summaries, 2)
			require.Contains(t, summaries[0], "Build SBOM")
			require.Contains(t, summaries[1], "`"+tc.packageName+"@"+tc.version+"`")
			require.Contains(t, stdout.String(), "NPM release build: "+tc.packageName+"@"+tc.version+"\n")
			require.Empty(t, stderr.String())
			require.Empty(t, annotations.String())
			require.Equal(t, canary, ownedTree(t, fsys.Path("canary")))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 3, "only package.json, tarball and SBOM remain, with no staging residue")
		})
	}
}

func TestNPMReleaseBuild_ScriptTypesRetainPostinstallBoundary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, scripts, message, stdout string
		repair                         bool
		calls                          [][]string
	}{
		{
			name: "selected numeric cannot be repaired by ci", scripts: `{"build":42,"lint":"eslint","postinstall":"repair-build"}`,
			message: "preflight npm build script", repair: true,
		},
		{
			name: "unused numeric still fails after ci", scripts: `{"build":"tsc","lint":42}`,
			message: "npm run build", stdout: "NPM release build: @org/app@1.0.0\n", calls: [][]string{{"ci"}, {"test"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			fsys.WriteFile("package.json", []byte(`{"name":"@org/app","version":"1.0.0","scripts":`+tc.scripts+`}`))
			fsys.WriteFile("org-app-1.0.0.tgz", []byte("prior archive"))
			bom := fsys.WriteFile("bom.json", []byte("prior SBOM"))
			require.NoError(t, os.Chmod(bom, 0o400))
			before := ownedTree(t, fsys.Root)
			npmCalls, npxRunner := &fakeNPMRunner{}, &fakeNPMRunner{}
			npmRunner := npmPreflightRunner(func(ctx context.Context, dir string, out, stderr io.Writer, args []string) error {
				if err := npmCalls.RunInherit(ctx, dir, out, stderr, args...); err != nil {
					return err
				}

				if args[0] == "ci" && tc.repair {
					fsys.WriteFile("package.json", []byte(`{"name":"@org/app","version":"1.0.0","scripts":{"build":"tsc","lint":"eslint"}}`))
				}

				return nil
			})
			summaryCalls := 0
			summary := npmPreflightSummary(func(context.Context, string) error {
				summaryCalls++

				return nil
			})

			var stdout, stderr, annotations bytes.Buffer

			err := appbuild.NPMReleaseBuild(t.Context(), summary, npmRunner, npxRunner, output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stderr, appbuild.NPMReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Root, EnableBuildSBOM: true},
				PackageScope:        "@org/", SBOMToolVersion: "4.2.1",
			})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			assert.Contains(t, err.Error(), tc.message)
			assert.Equal(t, tc.calls, npmCalls.calls)
			assert.Empty(t, npxRunner.calls)
			assert.Zero(t, summaryCalls)
			assert.Equal(t, tc.stdout, stdout.String())
			assert.Empty(t, stderr.String())
			assert.Empty(t, annotations.String())
			assert.Equal(t, before, ownedTree(t, fsys.Root))
		})
	}
}

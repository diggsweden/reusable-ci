// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

type fakeImageEvidenceBuildah struct {
	imageRef       string
	layout         string
	manifest       string
	manifestLayout string
	fail           bool
}

func (f *fakeImageEvidenceBuildah) ExportLocalImageToLayout(_ context.Context, imageRef, layoutDir string, _ io.Writer) error {
	f.imageRef = imageRef

	f.layout = layoutDir
	if f.fail {
		return errors.New("buildah failed") //nolint:err113 // test double error.
	}

	return os.WriteFile(filepath.Join(layoutDir, "index.json"), []byte("{}"), 0o600) //nolint:gosec,mnd // test fixture.
}

func (f *fakeImageEvidenceBuildah) ExportLocalManifestToLayout(_ context.Context, manifest, layoutDir string, _ io.Writer) error {
	f.manifest = manifest

	f.manifestLayout = layoutDir
	if f.fail {
		return errors.New("buildah failed") //nolint:err113 // test double error.
	}

	return os.WriteFile(filepath.Join(layoutDir, "index.json"), []byte("{}"), 0o600) //nolint:gosec,mnd // test fixture.
}

type fakeImageEvidenceSkopeoCopy struct {
	sourceLayout string
	ref          string
	digest       string
	destLayout   string
	osName       string
	arch         string
}

type fakeImageEvidenceSkopeo struct {
	localCopies    []fakeImageEvidenceSkopeoCopy
	registryCopies []fakeImageEvidenceSkopeoCopy
	fail           bool
}

func (f *fakeImageEvidenceSkopeo) CopyOCILayoutToOCILayout(_ context.Context, sourceLayout, destLayout, osName, arch string, _ io.Writer) error {
	f.localCopies = append(f.localCopies, fakeImageEvidenceSkopeoCopy{sourceLayout: sourceLayout, destLayout: destLayout, osName: osName, arch: arch})
	if f.fail {
		return errors.New("skopeo failed") //nolint:err113 // test double error.
	}

	return os.WriteFile(filepath.Join(destLayout, "index.json"), []byte("{}"), 0o600) //nolint:gosec,mnd // test fixture.
}

func (f *fakeImageEvidenceSkopeo) CopyDockerDigestToOCILayout(_ context.Context, ref, digest, destLayout, osName, arch string, _ io.Writer) error {
	f.registryCopies = append(f.registryCopies, fakeImageEvidenceSkopeoCopy{ref: ref, digest: digest, destLayout: destLayout, osName: osName, arch: arch})
	if f.fail {
		return errors.New("skopeo failed") //nolint:err113 // test double error.
	}

	return os.WriteFile(filepath.Join(destLayout, "index.json"), []byte("{}"), 0o600) //nolint:gosec,mnd // test fixture.
}

type fakeImageEvidenceTrivy struct {
	args     []string
	calls    [][]string
	jsonBody string
	code     int
	noOutput bool
	// codes, when set, gives the exit code of each call in turn.
	codes []int
}

func (f *fakeImageEvidenceTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.args = append([]string{}, args...)

	f.calls = append(f.calls, append([]string{}, args...))

	code := f.code
	if index := len(f.calls) - 1; index < len(f.codes) {
		code = f.codes[index]
	}

	if f.noOutput {
		return code, nil
	}

	for i, arg := range args {
		if arg == "--output" && i+1 < len(args) {
			body := f.jsonBody
			if body == "" {
				body = `{"Results":[]}`
			}

			if err := os.WriteFile(args[i+1], []byte(body), 0o600); err != nil { //nolint:gosec,mnd // test fixture.
				return 1, err
			}
		}
	}

	return code, nil
}

type fakeImageEvidenceSyft struct {
	target   string // the last target, for single-platform tests
	targets  []string
	outputs  map[string]string
	noOutput bool
	jsonBody string
}

func (f *fakeImageEvidenceSyft) Generate(_ context.Context, target string, outputs map[string]string, _ io.Writer) error {
	f.target = target
	f.targets = append(f.targets, target)

	f.outputs = outputs
	if f.noOutput {
		return nil
	}

	for _, path := range outputs {
		body := f.jsonBody
		if body == "" {
			body = `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`
		}

		if err := os.WriteFile(path, []byte(body), 0o600); err != nil { //nolint:gosec,mnd // test fixture.
			return err
		}
	}

	return nil
}

func TestImageEvidence_OCILayoutScansAndWritesOptionalSBOM(t *testing.T) {
	t.Parallel()
	work := t.TempDir()

	layout := filepath.Join(work, "layout")
	if err := os.Mkdir(layout, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	trivyOut := filepath.Join(work, "trivy.json")
	sbomOut := filepath.Join(work, "sbom.json")
	trivy := &fakeImageEvidenceTrivy{}
	syft := &fakeImageEvidenceSyft{}

	if err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, &fakeImageEvidenceSkopeo{}, trivy, syft, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		OCILayout:    layout,
		TrivyOutput:  trivyOut,
		SBOMOutput:   sbomOut,
		TrivyTimeout: "7m",
	}); err != nil {
		t.Fatal(err)
	}

	stagedTrivy := trivyArg(t, trivy.args, "--output")
	assertPrivateEvidenceOutput(t, stagedTrivy, trivyOut)

	wantArgs := []string{"image", "--input", layout, "--scanners", "vuln", "--skip-version-check", "--timeout", "7m", "--format", "json", "--output", stagedTrivy}
	if !slices.Equal(trivy.args, wantArgs) {
		t.Fatalf("trivy args = %#v, want %#v", trivy.args, wantArgs)
	}

	assertPrivateEvidenceOutput(t, syft.outputs["cyclonedx-json"], sbomOut)

	if syft.target != "oci-dir:"+layout {
		t.Fatalf("syft target=%q outputs=%v", syft.target, syft.outputs)
	}
}

func TestImageEvidence_LocalImageExportsTemporaryLayoutAndCleansUp(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	buildah := &fakeImageEvidenceBuildah{}
	trivy := &fakeImageEvidenceTrivy{}

	if err := appcontainer.ImageEvidence(context.Background(), buildah, &fakeImageEvidenceSkopeo{}, trivy, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		LocalImageRef: "localhost/example:test",
		TrivyOutput:   filepath.Join(work, "trivy.json"),
		TempDir:       work,
	}); err != nil {
		t.Fatal(err)
	}

	if buildah.imageRef != "localhost/example:test" || buildah.layout == "" {
		t.Fatalf("buildah image=%q layout=%q", buildah.imageRef, buildah.layout)
	}

	if !strings.HasPrefix(buildah.layout, work+string(os.PathSeparator)) {
		t.Fatalf("layout %q not under temp root %q", buildah.layout, work)
	}

	if _, err := os.Stat(buildah.layout); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary layout still exists or unexpected stat err: %v", err)
	}

	if len(trivy.args) < 3 || trivy.args[2] != buildah.layout {
		t.Fatalf("trivy args did not scan exported layout: %v", trivy.args)
	}
}

func TestImageEvidence_ValidatesReportBeforePublication(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"empty-object", `{}`, false},
		{"unknown-only", `{"unexpected":true}`, false},
		{"unnamed-null", `{"Results":null}`, false},
		{"blank-name", `{"ArtifactName":" \t"}`, false},
		{"typed-field", `{"Results":[{"Vulnerabilities":"invalid"}]}`, false},
		{"syntax", `{"Results":[`, false},
		{"empty-array", `{"Results":[],"Future":true}`, true},
		{"named-omitted", `{"ArtifactName":"image"}`, true},
		{"named-null", `{"ArtifactName":"image","Results":null}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()

			trivyPath, sbomPath := filepath.Join(root, "trivy.json"), filepath.Join(root, "sbom.json")
			for _, path := range []string{trivyPath, sbomPath} {
				require.NoError(t, os.WriteFile(path, []byte("OLD-CANARY"), 0o600))
			}

			before := snapshotContainerTree(t, root)
			buildah := &fakeImageEvidenceBuildah{}
			trivy, syft := &fakeImageEvidenceTrivy{jsonBody: tc.body}, &fakeImageEvidenceSyft{}

			var out bytes.Buffer

			err := appcontainer.ImageEvidence(t.Context(), buildah, nil, trivy, syft, &out, &out, appcontainer.ImageEvidenceInput{LocalImageRef: "localhost/app:test", TrivyOutput: trivyPath, SBOMOutput: sbomPath, TempDir: root})
			require.Len(t, trivy.calls, 1)
			require.NotEmpty(t, buildah.layout)
			_, statErr := os.Stat(buildah.layout)
			require.ErrorIs(t, statErr, os.ErrNotExist)
			staged := trivyArg(t, trivy.args, "--output")
			require.NotEqual(t, trivyPath, staged)
			_, statErr = os.Stat(filepath.Dir(staged))
			require.ErrorIs(t, statErr, os.ErrNotExist)

			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, []string{"oci-dir:" + buildah.layout}, syft.targets)

				body, readErr := os.ReadFile(trivyPath)
				require.NoError(t, readErr)
				require.Equal(t, tc.body, string(body))
				body, readErr = os.ReadFile(sbomPath)
				require.NoError(t, readErr)
				require.JSONEq(t, `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`, string(body))

				entries, readErr := os.ReadDir(root)
				require.NoError(t, readErr)
				require.Len(t, entries, 2, "only published reports should remain")
			} else {
				require.ErrorIs(t, err, errs.ErrMalformedInput)
				require.Empty(t, syft.targets)
				require.Equal(t, before, snapshotContainerTree(t, root))
			}

			if tc.name == "typed-field" {
				var cause *json.UnmarshalTypeError
				require.ErrorAs(t, err, &cause)
			}

			if tc.name == "syntax" {
				var cause *json.SyntaxError
				require.ErrorAs(t, err, &cause)
			}
		})
	}
}

// assertNoEvidenceToolsRan requires that a refused input reached none of the
// four tools. Each rejection test used to check a different subset -- one
// looked at the image export and trivy, the other at the manifest export,
// skopeo and trivy -- and neither looked at syft, so an input refused only
// after an SBOM had been generated would have passed both.
func assertNoEvidenceToolsRan(t *testing.T, buildah *fakeImageEvidenceBuildah, skopeo *fakeImageEvidenceSkopeo, trivy *fakeImageEvidenceTrivy, syft *fakeImageEvidenceSyft) {
	t.Helper()

	if buildah.imageRef != "" || buildah.manifest != "" {
		t.Errorf("buildah exported before validation: image=%q manifest=%q", buildah.imageRef, buildah.manifest)
	}

	if len(skopeo.localCopies) != 0 || len(skopeo.registryCopies) != 0 {
		t.Errorf("skopeo copied before validation: local=%+v registry=%+v", skopeo.localCopies, skopeo.registryCopies)
	}

	if len(trivy.calls) != 0 {
		t.Errorf("trivy scanned before validation: %v", trivy.calls)
	}

	if len(syft.targets) != 0 {
		t.Errorf("syft generated an SBOM before validation: %v", syft.targets)
	}
}

func TestImageEvidence_RejectsInvalidInputsBeforeTools(t *testing.T) {
	t.Parallel()
	work := t.TempDir()

	layout := filepath.Join(work, "layout")
	if err := os.Mkdir(layout, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   appcontainer.ImageEvidenceInput
		want error
	}{
		{name: "no source", in: appcontainer.ImageEvidenceInput{TrivyOutput: filepath.Join(work, "trivy.json")}, want: errs.ErrUsage},
		{name: "two sources", in: appcontainer.ImageEvidenceInput{OCILayout: layout, LocalImageRef: "local:test", TrivyOutput: filepath.Join(work, "trivy.json")}, want: errs.ErrUsage},
		{name: "no trivy output", in: appcontainer.ImageEvidenceInput{OCILayout: layout, SBOMOutput: filepath.Join(work, "sbom.json")}, want: errs.ErrUsage},
		{name: "missing layout", in: appcontainer.ImageEvidenceInput{OCILayout: filepath.Join(work, "missing"), TrivyOutput: filepath.Join(work, "trivy.json")}, want: errs.ErrMissingInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildah := &fakeImageEvidenceBuildah{}
			skopeo := &fakeImageEvidenceSkopeo{}
			trivy := &fakeImageEvidenceTrivy{}
			syft := &fakeImageEvidenceSyft{}

			err := appcontainer.ImageEvidence(context.Background(), buildah, skopeo, trivy, syft, io.Discard, io.Discard, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}

			assertNoEvidenceToolsRan(t, buildah, skopeo, trivy, syft)
		})
	}
}

// TestImageEvidence_ScansEachPlatformFromItsOwnLayout covers a multi-arch run
// from a local manifest: the manifest is exported once, split into a layout per
// platform, and each platform is scanned from its own layout.
//
// That last part is the claim worth having. Per-platform output files prove
// only that each scan was told where to write; scanning one platform's layout
// twice writes both files just as happily. Every scan is now tied back to the
// layout skopeo produced for that architecture.
func TestImageEvidence_ScansEachPlatformFromItsOwnLayout(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	buildah := &fakeImageEvidenceBuildah{}
	skopeo := &fakeImageEvidenceSkopeo{}
	trivy := &fakeImageEvidenceTrivy{}
	syft := &fakeImageEvidenceSyft{}

	err := appcontainer.ImageEvidence(context.Background(), buildah, skopeo, trivy, syft, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		LocalManifest:       "localhost/example:candidate",
		RegistryRef:         "registry.example/owner/example:final",
		Digest:              "sha256:" + strings.Repeat("a", 64),
		Platforms:           []string{"linux/amd64", "linux/arm64"},
		TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json"),
		SBOMOutputTemplate:  filepath.Join(work, "sbom-{platform}.json"),
		TempDir:             work,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("exports the local manifest and does not reach for the registry", func(t *testing.T) {
		if buildah.manifest != "localhost/example:candidate" || buildah.manifestLayout == "" {
			t.Errorf("buildah manifest=%q layout=%q", buildah.manifest, buildah.manifestLayout)
		}

		if len(skopeo.registryCopies) != 0 {
			t.Errorf("registry fallback used despite a local manifest: %+v", skopeo.registryCopies)
		}
	})

	t.Run("splits the manifest into one layout per platform", func(t *testing.T) {
		assertPlatformLayouts(t, buildah.manifestLayout, skopeo.localCopies)
	})

	t.Run("scans each platform from the layout made for it", func(t *testing.T) {
		assertPerPlatformScans(t, trivy, syft, skopeo.localCopies, work)
	})

	t.Run("writes the evidence and removes every temporary layout", func(t *testing.T) {
		assertEvidenceWrittenAndCleanedUp(t, buildah.manifestLayout, skopeo.localCopies, work)
	})
}

// assertPlatformLayouts requires one layout per platform, each split from the
// single exported manifest layout.
func assertPlatformLayouts(t *testing.T, manifestLayout string, copies []fakeImageEvidenceSkopeoCopy) {
	t.Helper()

	if len(copies) != 2 {
		t.Fatalf("local copies = %d, want one per platform", len(copies))
	}

	for i, arch := range []string{"amd64", "arm64"} {
		if copies[i].sourceLayout != manifestLayout {
			t.Errorf("copy[%d] source = %q, want the exported manifest layout %q", i, copies[i].sourceLayout, manifestLayout)
		}

		if copies[i].osName != "linux" || copies[i].arch != arch {
			t.Errorf("copy[%d] platform = %s/%s, want linux/%s", i, copies[i].osName, copies[i].arch, arch)
		}
	}
}

// assertPerPlatformScans ties every scan back to the layout built for that
// architecture. Per-platform output files prove only that each scan was told
// where to write; scanning one platform twice writes both files just as well.
func assertPerPlatformScans(t *testing.T, trivy *fakeImageEvidenceTrivy, syft *fakeImageEvidenceSyft, copies []fakeImageEvidenceSkopeoCopy, work string) {
	t.Helper()

	if len(trivy.calls) != 2 || len(copies) != 2 {
		t.Fatalf("trivy calls = %d, copies = %d, want 2 of each", len(trivy.calls), len(copies))
	}

	if len(syft.targets) != 2 {
		t.Fatalf("syft targets = %v, want one per platform", syft.targets)
	}

	for i, arch := range []string{"amd64", "arm64"} {
		layout := copies[i].destLayout

		if got := trivyArg(t, trivy.calls[i], "--input"); got != layout {
			t.Errorf("trivy scanned %q for %s, want that platform's layout %q", got, arch, layout)
		}

		assertPrivateEvidenceOutput(t, trivyArg(t, trivy.calls[i], "--output"), filepath.Join(work, "trivy-"+arch+".json"))

		if want := "oci-dir:" + layout; syft.targets[i] != want {
			t.Errorf("syft scanned %q for %s, want %q", syft.targets[i], arch, want)
		}
	}
}

// assertEvidenceWrittenAndCleanedUp checks the evidence files exist and that no
// temporary layout survives. The layouts are working copies of a whole image;
// leaving them behind fills the runner's disk one release at a time.
func assertEvidenceWrittenAndCleanedUp(t *testing.T, manifestLayout string, copies []fakeImageEvidenceSkopeoCopy, work string) {
	t.Helper()

	for _, name := range []string{"trivy-amd64.json", "trivy-arm64.json", "sbom-linux-amd64.json", "sbom-linux-arm64.json"} {
		if _, err := os.Stat(filepath.Join(work, name)); err != nil {
			t.Errorf("evidence %s missing: %v", name, err)
		}
	}

	layouts := make([]string, 0, 1+len(copies))
	layouts = append(layouts, manifestLayout)

	for _, copied := range copies {
		layouts = append(layouts, copied.destLayout)
	}

	for _, layout := range layouts {
		if _, err := os.Stat(layout); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temporary layout %s was not cleaned up: %v", layout, err)
		}
	}
}

// trivyArg returns the value following flag in a recorded trivy invocation.
func trivyArg(t *testing.T, args []string, flag string) string {
	t.Helper()

	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	t.Fatalf("trivy called without %s: %v", flag, args)

	return ""
}

func TestImageEvidence_MultiArchPrefersScanLayoutOverOtherSources(t *testing.T) {
	t.Parallel()
	work := t.TempDir()

	scanLayout := filepath.Join(work, "scan-layout")
	if err := os.Mkdir(scanLayout, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	buildah := &fakeImageEvidenceBuildah{}
	skopeo := &fakeImageEvidenceSkopeo{}

	err := appcontainer.ImageEvidence(context.Background(), buildah, skopeo, &fakeImageEvidenceTrivy{}, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		ScanLayout:          scanLayout,
		LocalManifest:       "localhost/ignored:candidate",
		RegistryRef:         "registry.example/owner/example:final",
		Digest:              "sha256:" + strings.Repeat("a", 64),
		Platforms:           []string{"linux/amd64"},
		TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json"),
		TempDir:             work,
	})
	if err != nil {
		t.Fatal(err)
	}

	if buildah.manifest != "" {
		t.Fatalf("buildah local manifest was exported despite scan layout: %q", buildah.manifest)
	}

	if len(skopeo.registryCopies) != 0 {
		t.Fatalf("registry fallback used despite scan layout: %+v", skopeo.registryCopies)
	}

	if len(skopeo.localCopies) != 1 || skopeo.localCopies[0].sourceLayout != scanLayout {
		t.Fatalf("local copies = %+v, want source %q", skopeo.localCopies, scanLayout)
	}

	if _, err := os.Stat(scanLayout); err != nil {
		t.Fatalf("caller-owned scan layout was removed: %v", err)
	}
}

func TestImageEvidence_MultiArchRegistryDigestFallback(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	skopeo := &fakeImageEvidenceSkopeo{}

	err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, skopeo, &fakeImageEvidenceTrivy{}, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		RegistryRef:         "registry.example/owner/example:final",
		Digest:              "sha256:" + strings.Repeat("a", 64),
		Platforms:           []string{"linux/amd64"},
		TrivyOutputTemplate: filepath.Join(work, "trivy-{platform}.json"),
		TempDir:             work,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(skopeo.localCopies) != 0 {
		t.Fatalf("local source used for registry fallback: %+v", skopeo.localCopies)
	}

	if len(skopeo.registryCopies) != 1 {
		t.Fatalf("registry copies = %d, want 1", len(skopeo.registryCopies))
	}

	copied := skopeo.registryCopies[0]
	if copied.ref != "registry.example/owner/example:final" || copied.digest != "sha256:"+strings.Repeat("a", 64) || copied.osName != "linux" || copied.arch != "amd64" {
		t.Fatalf("registry copy = %+v", copied)
	}

	if _, err := os.Stat(filepath.Join(work, "trivy-linux-amd64.json")); err != nil {
		t.Fatalf("trivy output missing: %v", err)
	}
}

// TestImageEvidence_ScansTheRegistryImageItPulled covers the registry fallback:
// with no local image or manifest, the named platform is copied out of the
// registry by digest into a temporary layout, scanned there, and the layout
// removed.
//
// The scans are tied to that layout. Finding the trivy output and the SBOM
// output only shows each tool was told where to write -- the same gap its
// multi-arch sibling had until 22ccc7b0, closed here too.
func TestImageEvidence_ScansTheRegistryImageItPulled(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	skopeo := &fakeImageEvidenceSkopeo{}
	trivy := &fakeImageEvidenceTrivy{}
	syft := &fakeImageEvidenceSyft{}
	digest := "sha256:" + strings.Repeat("d", 64)
	trivyOutput := filepath.Join(work, "trivy.json")
	sbomOutput := filepath.Join(work, "sbom.json")

	err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, skopeo, trivy, syft, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		RegistryDigestRef: "registry.example/owner/example@" + digest,
		Platforms:         []string{"linux/amd64"},
		TrivyOutput:       trivyOutput,
		SBOMOutput:        sbomOutput,
		TempDir:           work,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(skopeo.registryCopies) != 1 {
		t.Fatalf("registry copies = %+v, want exactly one", skopeo.registryCopies)
	}

	copied := skopeo.registryCopies[0]

	// Pulled by digest, with the tag stripped: what is scanned is the image
	// the digest names, not whatever the tag points at now.
	if copied.ref != "registry.example/owner/example" || copied.digest != digest {
		t.Errorf("copied %s@%s, want registry.example/owner/example@%s", copied.ref, copied.digest, digest)
	}

	if copied.osName != "linux" || copied.arch != "amd64" {
		t.Errorf("copied platform = %s/%s, want linux/amd64", copied.osName, copied.arch)
	}

	if len(trivy.calls) != 1 {
		t.Fatalf("trivy calls = %d, want exactly one", len(trivy.calls))
	}

	if got := trivyArg(t, trivy.calls[0], "--input"); got != copied.destLayout {
		t.Errorf("trivy scanned %q, want the layout pulled for it %q", got, copied.destLayout)
	}

	assertPrivateEvidenceOutput(t, trivyArg(t, trivy.calls[0], "--output"), trivyOutput)

	if want := []string{"oci-dir:" + copied.destLayout}; !slices.Equal(syft.targets, want) {
		t.Errorf("syft scanned %v, want %v", syft.targets, want)
	}

	assertPrivateEvidenceOutput(t, syft.outputs["cyclonedx-json"], sbomOutput)

	// A whole image was unpacked to scan it; leaving it behind fills the
	// runner's disk one release at a time.
	if _, err := os.Stat(copied.destLayout); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary registry layout was not cleaned up: %v", err)
	}
}

func TestImageEvidence_RejectsInvalidMultiArchInputsBeforeTools(t *testing.T) {
	t.Parallel()
	work := t.TempDir()

	layout := filepath.Join(work, "layout")
	if err := os.Mkdir(layout, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   appcontainer.ImageEvidenceInput
		want error
	}{
		{name: "no multi source", in: appcontainer.ImageEvidenceInput{Platforms: []string{"linux/amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrUsage},
		{name: "no platforms", in: appcontainer.ImageEvidenceInput{LocalManifest: "local", TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrUsage},
		{name: "constant template", in: appcontainer.ImageEvidenceInput{LocalManifest: "local", Platforms: []string{"linux/amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy.json")}, want: errs.ErrUsage},
		{name: "duplicate template", in: appcontainer.ImageEvidenceInput{LocalManifest: "local", Platforms: []string{"linux/amd64", "windows/amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrUsage},
		{name: "invalid platform", in: appcontainer.ImageEvidenceInput{LocalManifest: "local", Platforms: []string{"amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrUsage},
		{name: "mixed single and multi flags", in: appcontainer.ImageEvidenceInput{OCILayout: layout, LocalManifest: "local", Platforms: []string{"linux/amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrUsage},
		{name: "missing scan layout", in: appcontainer.ImageEvidenceInput{ScanLayout: filepath.Join(work, "missing"), Platforms: []string{"linux/amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrMissingInput},
		{name: "invalid registry digest ref", in: appcontainer.ImageEvidenceInput{RegistryDigestRef: "registry.example/owner/example:tag", Platforms: []string{"linux/amd64"}, TrivyOutputTemplate: filepath.Join(work, "trivy-{arch}.json")}, want: errs.ErrUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildah := &fakeImageEvidenceBuildah{}
			skopeo := &fakeImageEvidenceSkopeo{}
			trivy := &fakeImageEvidenceTrivy{}
			syft := &fakeImageEvidenceSyft{}

			err := appcontainer.ImageEvidence(context.Background(), buildah, skopeo, trivy, syft, io.Discard, io.Discard, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}

			assertNoEvidenceToolsRan(t, buildah, skopeo, trivy, syft)
		})
	}
}

// TestImageEvidence_OnePlatformFailingLeavesItsPeersAndNoLayouts covers a
// multi-arch run in which scanning fails. A failed scan of one platform does
// not stop the next: every platform is scanned, the first failure is the one
// returned, only the platform that succeeded publishes evidence, and no
// temporary layout survives either outcome. Failing to extract a platform
// layout is different: it aborts the run before any scan, and still cleans up.
func TestImageEvidence_OnePlatformFailingLeavesItsPeersAndNoLayouts(t *testing.T) {
	t.Parallel()

	input := func(work string) appcontainer.ImageEvidenceInput {
		return appcontainer.ImageEvidenceInput{
			LocalManifest:       "localhost/example:candidate",
			Platforms:           []string{"linux/amd64", "linux/arm64", "linux/s390x"},
			TrivyOutputTemplate: filepath.Join(work, "evidence", "trivy-{arch}.json"),
			SBOMOutputTemplate:  filepath.Join(work, "evidence", "sbom-{arch}.json"),
			TempDir:             filepath.Join(work, "tmp"),
		}
	}

	prepare := func(t *testing.T) string {
		t.Helper()

		work := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(work, "evidence"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(work, "tmp"), 0o750))

		return work
	}

	t.Run("scan failures", func(t *testing.T) {
		t.Parallel()

		work := prepare(t)
		skopeo := &fakeImageEvidenceSkopeo{}
		trivy := &fakeImageEvidenceTrivy{codes: []int{3, 0, 5}}
		syft := &fakeImageEvidenceSyft{}

		err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, skopeo, trivy, syft, io.Discard, io.Discard, input(work))
		require.ErrorIs(t, err, errs.ErrDependencyUnavailable)
		require.Contains(t, err.Error(), "exited with status 3")
		require.NotContains(t, err.Error(), "status 5")

		require.Len(t, trivy.calls, 3, "every platform is scanned")
		require.Equal(t, []string{"oci-dir:" + skopeo.localCopies[1].destLayout}, syft.targets, "only the platform whose scan passed gets an SBOM")

		entries, readErr := os.ReadDir(filepath.Join(work, "evidence"))
		require.NoError(t, readErr)

		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}

		require.ElementsMatch(t, []string{"trivy-arm64.json", "sbom-arm64.json"}, names)

		tmp, tmpErr := os.ReadDir(filepath.Join(work, "tmp"))
		require.NoError(t, tmpErr)
		require.Empty(t, tmp, "temporary layouts survived")
	})

	t.Run("layout extraction failure", func(t *testing.T) {
		t.Parallel()

		work := prepare(t)
		skopeo := &fakeImageEvidenceSkopeo{fail: true}
		trivy := &fakeImageEvidenceTrivy{}

		err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, skopeo, trivy, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, input(work))
		require.Error(t, err)
		require.Len(t, skopeo.localCopies, 1, "extraction failure aborts before the next platform")
		require.Empty(t, trivy.calls)

		tmp, tmpErr := os.ReadDir(filepath.Join(work, "tmp"))
		require.NoError(t, tmpErr)
		require.Empty(t, tmp, "temporary layouts survived")
	})
}

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
}

func (f *fakeImageEvidenceTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.args = append([]string{}, args...)

	f.calls = append(f.calls, append([]string{}, args...))
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

	return f.code, nil
}

type fakeImageEvidenceSyft struct {
	target  string // the last target, for single-platform tests
	targets []string
	outputs map[string]string
}

func (f *fakeImageEvidenceSyft) Generate(_ context.Context, target string, outputs map[string]string, _ io.Writer) error {
	f.target = target
	f.targets = append(f.targets, target)

	f.outputs = outputs
	for _, path := range outputs {
		if err := os.WriteFile(path, []byte(`{"bomFormat":"CycloneDX"}`), 0o600); err != nil { //nolint:gosec,mnd // test fixture.
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

	wantArgs := []string{"image", "--input", layout, "--scanners", "vuln", "--skip-version-check", "--timeout", "7m", "--format", "json", "--output", trivyOut}
	if !reflect.DeepEqual(trivy.args, wantArgs) {
		t.Fatalf("trivy args = %#v, want %#v", trivy.args, wantArgs)
	}

	if syft.target != "oci-dir:"+layout || syft.outputs["cyclonedx-json"] != sbomOut {
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

func TestImageEvidence_RejectsInvalidTrivyShape(t *testing.T) {
	t.Parallel()
	work := t.TempDir()

	layout := filepath.Join(work, "layout")
	if err := os.Mkdir(layout, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, &fakeImageEvidenceSkopeo{}, &fakeImageEvidenceTrivy{jsonBody: `{"unexpected":true}`}, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
		OCILayout:   layout,
		TrivyOutput: filepath.Join(work, "trivy.json"),
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
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
		Digest:              "sha256:reuse",
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

		if got := trivyArg(t, trivy.calls[i], "--output"); got != filepath.Join(work, "trivy-"+arch+".json") {
			t.Errorf("trivy wrote %q for %s", got, arch)
		}

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
		Digest:              "sha256:ignored",
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
		Digest:              "sha256:reused",
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
	if copied.ref != "registry.example/owner/example:final" || copied.digest != "sha256:reused" || copied.osName != "linux" || copied.arch != "amd64" {
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

	if got := trivyArg(t, trivy.calls[0], "--output"); got != trivyOutput {
		t.Errorf("trivy wrote %q, want %q", got, trivyOutput)
	}

	if want := []string{"oci-dir:" + copied.destLayout}; !reflect.DeepEqual(syft.targets, want) {
		t.Errorf("syft scanned %v, want %v", syft.targets, want)
	}

	if syft.outputs["cyclonedx-json"] != sbomOutput {
		t.Errorf("syft outputs = %v, want the SBOM at %s", syft.outputs, sbomOutput)
	}

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

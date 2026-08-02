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
	target  string
	outputs map[string]string
}

func (f *fakeImageEvidenceSyft) Generate(_ context.Context, target string, outputs map[string]string, _ io.Writer) error {
	f.target = target

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
		{name: "no trivy output", in: appcontainer.ImageEvidenceInput{OCILayout: layout}, want: errs.ErrUsage},
		{name: "missing layout", in: appcontainer.ImageEvidenceInput{OCILayout: filepath.Join(work, "missing"), TrivyOutput: filepath.Join(work, "trivy.json")}, want: errs.ErrMissingInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildah := &fakeImageEvidenceBuildah{}
			trivy := &fakeImageEvidenceTrivy{}

			err := appcontainer.ImageEvidence(context.Background(), buildah, &fakeImageEvidenceSkopeo{}, trivy, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}

			if buildah.imageRef != "" || len(trivy.args) != 0 {
				t.Fatalf("tools invoked before validation: buildah=%q trivy=%v", buildah.imageRef, trivy.args)
			}
		})
	}
}

func TestImageEvidence_MultiArchLocalManifestExportsAndScansPerPlatform(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	buildah := &fakeImageEvidenceBuildah{}
	skopeo := &fakeImageEvidenceSkopeo{}
	trivy := &fakeImageEvidenceTrivy{}

	err := appcontainer.ImageEvidence(context.Background(), buildah, skopeo, trivy, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
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

	if buildah.manifest != "localhost/example:candidate" || buildah.manifestLayout == "" {
		t.Fatalf("buildah manifest=%q layout=%q", buildah.manifest, buildah.manifestLayout)
	}

	if _, err := os.Stat(buildah.manifestLayout); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary full layout still exists or unexpected stat err: %v", err)
	}

	if len(skopeo.registryCopies) != 0 {
		t.Fatalf("registry fallback used despite local manifest: %+v", skopeo.registryCopies)
	}

	if len(skopeo.localCopies) != 2 {
		t.Fatalf("local copies = %d, want 2", len(skopeo.localCopies))
	}

	for i, arch := range []string{"amd64", "arm64"} {
		copied := skopeo.localCopies[i]
		if copied.sourceLayout != buildah.manifestLayout || copied.osName != "linux" || copied.arch != arch {
			t.Fatalf("copy[%d] = %+v", i, copied)
		}

		if _, err := os.Stat(copied.destLayout); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary %s layout still exists or unexpected stat err: %v", arch, err)
		}

		if _, err := os.Stat(filepath.Join(work, "trivy-"+arch+".json")); err != nil {
			t.Fatalf("trivy output for %s missing: %v", arch, err)
		}
	}

	for _, name := range []string{"sbom-linux-amd64.json", "sbom-linux-arm64.json"} {
		if _, err := os.Stat(filepath.Join(work, name)); err != nil {
			t.Fatalf("sbom output %s missing: %v", name, err)
		}
	}

	if len(trivy.calls) != 2 {
		t.Fatalf("trivy calls = %d, want 2", len(trivy.calls))
	}
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

func TestImageEvidence_RegistryDigestRefScansSinglePlatformAndWritesSBOM(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	skopeo := &fakeImageEvidenceSkopeo{}
	syft := &fakeImageEvidenceSyft{}
	digest := "sha256:" + strings.Repeat("d", 64)
	trivyOutput := filepath.Join(work, "trivy.json")
	sbomOutput := filepath.Join(work, "sbom.json")

	err := appcontainer.ImageEvidence(context.Background(), &fakeImageEvidenceBuildah{}, skopeo, &fakeImageEvidenceTrivy{}, syft, io.Discard, io.Discard, appcontainer.ImageEvidenceInput{
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
		t.Fatalf("registry copies = %d, want 1", len(skopeo.registryCopies))
	}

	copied := skopeo.registryCopies[0]
	if copied.ref != "registry.example/owner/example" || copied.digest != digest || copied.osName != "linux" || copied.arch != "amd64" {
		t.Fatalf("registry copy = %+v", copied)
	}

	if _, err := os.Stat(copied.destLayout); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary registry layout still exists or unexpected stat err: %v", err)
	}

	if _, err := os.Stat(trivyOutput); err != nil {
		t.Fatalf("trivy output missing: %v", err)
	}

	if syft.outputs["cyclonedx-json"] != sbomOutput {
		t.Fatalf("syft outputs = %v", syft.outputs)
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

			err := appcontainer.ImageEvidence(context.Background(), buildah, skopeo, trivy, &fakeImageEvidenceSyft{}, io.Discard, io.Discard, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}

			if buildah.manifest != "" || len(skopeo.localCopies) != 0 || len(skopeo.registryCopies) != 0 || len(trivy.args) != 0 {
				t.Fatalf("tools invoked before validation: buildah=%q skopeo=%+v/%+v trivy=%v", buildah.manifest, skopeo.localCopies, skopeo.registryCopies, trivy.args)
			}
		})
	}
}

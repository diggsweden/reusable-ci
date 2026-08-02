// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const defaultImageEvidenceTrivyTimeout = "30m"

// ImageEvidenceBuildah is the Buildah surface needed for local-image evidence.
type ImageEvidenceBuildah interface {
	ExportLocalImageToLayout(ctx context.Context, imageRef, layoutDir string, out io.Writer) error
	ExportLocalManifestToLayout(ctx context.Context, manifest, layoutDir string, out io.Writer) error
}

// ImageEvidenceSkopeo is the Skopeo surface needed for per-platform OCI layouts.
type ImageEvidenceSkopeo interface {
	CopyOCILayoutToOCILayout(ctx context.Context, sourceLayout, destLayout, osName, arch string, errOut io.Writer) error
	CopyDockerDigestToOCILayout(ctx context.Context, ref, digest, destLayout, osName, arch string, errOut io.Writer) error
}

// ImageEvidenceTrivy is the Trivy surface needed for local OCI-layout evidence.
type ImageEvidenceTrivy interface {
	RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error)
}

// ImageEvidenceSyft is the Syft surface needed for optional CycloneDX output.
type ImageEvidenceSyft interface {
	Generate(ctx context.Context, target string, outputs map[string]string, errOut io.Writer) error
}

// ImageEvidenceInput drives local image evidence generation.
type ImageEvidenceInput struct {
	OCILayout           string
	LocalImageRef       string
	TrivyOutput         string
	SBOMOutput          string
	ScanLayout          string
	LocalManifest       string
	RegistryDigestRef   string
	RegistryRef         string
	Digest              string
	Platforms           []string
	TrivyOutputTemplate string
	SBOMOutputTemplate  string
	TrivyTimeout        string
	TempDir             string
}

// ImageEvidence scans an OCI layout with Trivy and optionally writes a CycloneDX SBOM with Syft.
func ImageEvidence(ctx context.Context, buildah ImageEvidenceBuildah, skopeo ImageEvidenceSkopeo, trivy ImageEvidenceTrivy, syft ImageEvidenceSyft, out, stderr io.Writer, in ImageEvidenceInput) error {
	if err := normalizeImageEvidenceRegistryDigestRef(&in); err != nil {
		return err
	}

	if imageEvidenceSingleRegistryDigestRequested(in) {
		return imageEvidenceSingleRegistryDigest(ctx, skopeo, trivy, syft, out, stderr, in)
	}

	if imageEvidenceMultiArchRequested(in) {
		return imageEvidenceMultiArch(ctx, buildah, skopeo, trivy, syft, out, stderr, in)
	}

	if err := validateSingleImageEvidenceInput(in); err != nil {
		return err
	}

	layout := in.OCILayout

	if in.LocalImageRef != "" {
		exported, cleanup, err := exportImageEvidenceLayout(ctx, buildah, out, in)
		if err != nil {
			return err
		}

		defer cleanup()

		layout = exported
	}

	return scanImageEvidenceLayout(ctx, trivy, syft, out, stderr, layout, in.TrivyOutput, in.SBOMOutput, trivyTimeoutValue(in.TrivyTimeout))
}

func imageEvidenceSingleRegistryDigestRequested(in ImageEvidenceInput) bool {
	return in.RegistryDigestRef != "" && in.TrivyOutput != "" && in.TrivyOutputTemplate == "" && in.SBOMOutputTemplate == "" && in.OCILayout == "" && in.LocalImageRef == "" && in.ScanLayout == "" && in.LocalManifest == ""
}

func imageEvidenceSingleRegistryDigest(ctx context.Context, skopeo ImageEvidenceSkopeo, trivy ImageEvidenceTrivy, syft ImageEvidenceSyft, out, stderr io.Writer, in ImageEvidenceInput) error {
	platforms, err := parseImageEvidencePlatforms(in.Platforms)
	if err != nil {
		return err
	}

	if len(platforms) != 1 {
		return fmt.Errorf("exactly one --platform is required with --registry-digest-ref and --trivy-output: %w", errs.ErrUsage)
	}

	layout, err := makeImageEvidenceTempDir(in, "image-evidence-registry.*")
	if err != nil {
		return err
	}

	defer func() { _ = os.RemoveAll(layout) }()

	platform := platforms[0]
	if err := skopeo.CopyDockerDigestToOCILayout(ctx, in.RegistryRef, in.Digest, layout, platform.OS, platform.Arch, stderr); err != nil {
		return err
	}

	return scanImageEvidenceLayout(ctx, trivy, syft, out, stderr, layout, in.TrivyOutput, in.SBOMOutput, trivyTimeoutValue(in.TrivyTimeout))
}

func validateSingleImageEvidenceInput(in ImageEvidenceInput) error {
	if (in.OCILayout == "") == (in.LocalImageRef == "") {
		return fmt.Errorf("set exactly one of --oci-layout or --local-image-ref: %w", errs.ErrUsage)
	}

	if in.TrivyOutput == "" {
		return fmt.Errorf("trivy output is required: %w", errs.ErrUsage)
	}

	if in.OCILayout != "" {
		if err := requireImageEvidenceDir(in.OCILayout, "oci layout"); err != nil {
			return err
		}
	}

	return nil
}

func imageEvidenceMultiArchRequested(in ImageEvidenceInput) bool {
	return in.ScanLayout != "" || in.LocalManifest != "" || in.RegistryDigestRef != "" || in.RegistryRef != "" || in.Digest != "" || len(in.Platforms) > 0 || in.TrivyOutputTemplate != "" || in.SBOMOutputTemplate != ""
}

func imageEvidenceMultiArch(ctx context.Context, buildah ImageEvidenceBuildah, skopeo ImageEvidenceSkopeo, trivy ImageEvidenceTrivy, syft ImageEvidenceSyft, out, stderr io.Writer, in ImageEvidenceInput) error {
	platforms, err := validateMultiArchImageEvidenceInput(in)
	if err != nil {
		return err
	}

	fullLayout, cleanup, err := imageEvidenceFullLayout(ctx, buildah, out, in)
	if err != nil {
		return err
	}
	defer cleanup()

	var firstScanErr error

	for _, platform := range platforms {
		scanErr, fatalErr := imageEvidenceScanOnePlatform(ctx, skopeo, trivy, syft, out, stderr, in, fullLayout, platform)
		if fatalErr != nil {
			return fatalErr
		}

		if scanErr != nil && firstScanErr == nil {
			firstScanErr = scanErr
		}
	}

	return firstScanErr
}

// imageEvidenceScanOnePlatform extracts one platform into a temp OCI layout
// and scans it. The first return value is a non-fatal scan/cleanup error (the
// caller keeps going); the second aborts the whole run.
func imageEvidenceScanOnePlatform(ctx context.Context, skopeo ImageEvidenceSkopeo, trivy ImageEvidenceTrivy, syft ImageEvidenceSyft, out, stderr io.Writer, in ImageEvidenceInput, fullLayout string, platform imageEvidencePlatform) (error, error) { //nolint:revive // (scanErr, fatalErr): non-fatal scan error vs run-aborting error.
	layout, err := makeImageEvidenceTempDir(in, "image-evidence-"+platform.Arch+".*")
	if err != nil {
		return nil, err
	}

	if fullLayout != "" {
		err = skopeo.CopyOCILayoutToOCILayout(ctx, fullLayout, layout, platform.OS, platform.Arch, stderr)
	} else {
		err = skopeo.CopyDockerDigestToOCILayout(ctx, in.RegistryRef, in.Digest, layout, platform.OS, platform.Arch, stderr)
	}

	if err != nil {
		_ = os.RemoveAll(layout)

		return nil, err
	}

	trivyOutput := renderImageEvidenceTemplate(in.TrivyOutputTemplate, platform)
	sbomOutput := renderImageEvidenceTemplate(in.SBOMOutputTemplate, platform)

	var scanErr error
	if err := scanImageEvidenceLayout(ctx, trivy, syft, out, stderr, layout, trivyOutput, sbomOutput, trivyTimeoutValue(in.TrivyTimeout)); err != nil {
		scanErr = err
	}

	if err := os.RemoveAll(layout); err != nil && scanErr == nil {
		scanErr = fmt.Errorf("remove image evidence temp layout: %w", err)
	}

	return scanErr, nil
}

func validateMultiArchImageEvidenceInput(in ImageEvidenceInput) ([]imageEvidencePlatform, error) {
	if in.OCILayout != "" || in.LocalImageRef != "" || in.TrivyOutput != "" || in.SBOMOutput != "" {
		return nil, fmt.Errorf("multi-arch image evidence uses --scan-layout/--local-manifest/--registry-ref with --trivy-output-template, not single-image source/output flags: %w", errs.ErrUsage)
	}

	platforms, err := parseImageEvidencePlatforms(in.Platforms)
	if err != nil {
		return nil, err
	}

	if err := validateMultiArchImageEvidenceTemplates(in, platforms); err != nil {
		return nil, err
	}

	if err := validateMultiArchImageEvidenceSource(in); err != nil {
		return nil, err
	}

	return platforms, nil
}

// validateMultiArchImageEvidenceTemplates checks the per-arch output templates.
func validateMultiArchImageEvidenceTemplates(in ImageEvidenceInput, platforms []imageEvidencePlatform) error {
	if in.TrivyOutputTemplate == "" {
		return fmt.Errorf("trivy output template is required: %w", errs.ErrUsage)
	}

	if err := validateImageEvidenceOutputTemplate(in.TrivyOutputTemplate, "trivy output template", platforms); err != nil {
		return err
	}

	if in.SBOMOutputTemplate != "" {
		if err := validateImageEvidenceOutputTemplate(in.SBOMOutputTemplate, "sbom output template", platforms); err != nil {
			return err
		}
	}

	return nil
}

// validateMultiArchImageEvidenceSource checks that exactly one usable image
// source was supplied.
func validateMultiArchImageEvidenceSource(in ImageEvidenceInput) error {
	if in.ScanLayout != "" {
		return requireImageEvidenceDir(in.ScanLayout, "scan layout")
	}

	if in.LocalManifest != "" {
		return nil
	}

	if in.RegistryRef == "" || in.Digest == "" {
		return fmt.Errorf("set --scan-layout, --local-manifest, --registry-digest-ref, or both --registry-ref and --digest for multi-arch image evidence: %w", errs.ErrUsage)
	}

	return nil
}

func normalizeImageEvidenceRegistryDigestRef(in *ImageEvidenceInput) error {
	if in.RegistryDigestRef == "" {
		return nil
	}

	ref, digest, err := splitImageEvidenceRegistryDigestRef(in.RegistryDigestRef)
	if err != nil {
		return err
	}

	if in.RegistryRef != "" && in.RegistryRef != ref {
		return fmt.Errorf("--registry-digest-ref conflicts with --registry-ref: %w", errs.ErrUsage)
	}

	if in.Digest != "" && in.Digest != digest {
		return fmt.Errorf("--registry-digest-ref conflicts with --digest: %w", errs.ErrUsage)
	}

	in.RegistryRef = ref
	in.Digest = digest

	return nil
}

func splitImageEvidenceRegistryDigestRef(value string) (string, string, error) {
	ref := strings.TrimSpace(value)
	if ref == "" || strings.ContainsAny(ref, " \t\n\r") {
		return "", "", fmt.Errorf("registry digest ref is empty or unsafe: %w", errs.ErrUsage)
	}

	imageRef, digest, ok := strings.Cut(ref, "@")
	if !ok || imageRef == "" || digest == "" || strings.Contains(digest, "@") {
		return "", "", fmt.Errorf("registry digest ref must be image@sha256:<digest>: %w", errs.ErrUsage)
	}

	if !domaincontainer.ValidDigest(digest) {
		return "", "", fmt.Errorf("registry digest ref has invalid digest %q: %w", digest, errs.ErrUsage)
	}

	return imageRef, digest, nil
}

func imageEvidenceFullLayout(ctx context.Context, buildah ImageEvidenceBuildah, out io.Writer, in ImageEvidenceInput) (string, func(), error) {
	if in.ScanLayout != "" {
		return in.ScanLayout, func() {}, nil
	}

	if in.LocalManifest == "" {
		return "", func() {}, nil
	}

	layout, err := makeImageEvidenceTempDir(in, "image-evidence-full.*")
	if err != nil {
		return "", nil, err
	}

	cleanup := func() { _ = os.RemoveAll(layout) }
	if err := buildah.ExportLocalManifestToLayout(ctx, in.LocalManifest, layout, out); err != nil {
		cleanup()

		return "", nil, err
	}

	return layout, cleanup, nil
}

type imageEvidencePlatform struct {
	Raw  string
	OS   string
	Arch string
}

func parseImageEvidencePlatforms(values []string) ([]imageEvidencePlatform, error) {
	raws := splitImageEvidenceList(values)
	if len(raws) == 0 {
		return nil, fmt.Errorf("at least one --platform is required for multi-arch image evidence: %w", errs.ErrUsage)
	}

	seen := make(map[string]bool, len(raws))

	platforms := make([]imageEvidencePlatform, 0, len(raws))
	for _, raw := range raws {
		osName, arch, ok := strings.Cut(raw, "/")
		if !ok || osName == "" || arch == "" || strings.Contains(arch, "/") {
			return nil, fmt.Errorf("platform %q must be os/arch, for example linux/amd64: %w", raw, errs.ErrUsage)
		}

		if seen[raw] {
			return nil, fmt.Errorf("duplicate platform %q: %w", raw, errs.ErrUsage)
		}

		seen[raw] = true
		platforms = append(platforms, imageEvidencePlatform{Raw: raw, OS: osName, Arch: arch})
	}

	return platforms, nil
}

func splitImageEvidenceList(values []string) []string {
	var out []string

	for _, value := range values {
		for _, field := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\t' || r == ' '
		}) {
			if field != "" {
				out = append(out, field)
			}
		}
	}

	return out
}

func validateImageEvidenceOutputTemplate(tpl, label string, platforms []imageEvidencePlatform) error {
	if !strings.Contains(tpl, "{arch}") && !strings.Contains(tpl, "{platform}") {
		return fmt.Errorf("%s must contain {arch} or {platform}: %w", label, errs.ErrUsage)
	}

	seen := make(map[string]bool, len(platforms))
	for _, platform := range platforms {
		path := renderImageEvidenceTemplate(tpl, platform)
		if path == "" {
			return fmt.Errorf("%s rendered an empty path for %s: %w", label, platform.Raw, errs.ErrUsage)
		}

		if seen[path] {
			return fmt.Errorf("%s renders duplicate path %q: %w", label, path, errs.ErrUsage)
		}

		seen[path] = true
	}

	return nil
}

func renderImageEvidenceTemplate(tpl string, platform imageEvidencePlatform) string {
	path := strings.ReplaceAll(tpl, "{platform}", strings.ReplaceAll(platform.Raw, "/", "-"))
	path = strings.ReplaceAll(path, "{os}", platform.OS)
	path = strings.ReplaceAll(path, "{arch}", platform.Arch)

	return path
}

func exportImageEvidenceLayout(ctx context.Context, buildah ImageEvidenceBuildah, out io.Writer, in ImageEvidenceInput) (string, func(), error) {
	layout, err := makeImageEvidenceTempDir(in, "image-evidence.*")
	if err != nil {
		return "", nil, fmt.Errorf("create image evidence temp layout: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(layout) }
	if err := buildah.ExportLocalImageToLayout(ctx, in.LocalImageRef, layout, out); err != nil {
		cleanup()

		return "", nil, err
	}

	return layout, cleanup, nil
}

func makeImageEvidenceTempDir(in ImageEvidenceInput, pattern string) (string, error) {
	tmpRoot := in.TempDir
	if tmpRoot == "" {
		tmpRoot = os.Getenv("RUNNER_TEMP")
	}

	layout, err := os.MkdirTemp(tmpRoot, pattern)
	if err != nil {
		return "", fmt.Errorf("create image evidence temp layout: %w", err)
	}

	return layout, nil
}

func scanImageEvidenceLayout(ctx context.Context, trivy ImageEvidenceTrivy, syft ImageEvidenceSyft, out, stderr io.Writer, layout, trivyOutput, sbomOutput, timeout string) error {
	if err := runImageEvidenceTrivy(ctx, trivy, out, stderr, layout, trivyOutput, timeout); err != nil {
		return err
	}

	if err := validateImageEvidenceTrivyOutput(trivyOutput); err != nil {
		return err
	}

	if sbomOutput != "" {
		if err := syft.Generate(ctx, "oci-dir:"+layout, map[string]string{"cyclonedx-json": sbomOutput}, stderr); err != nil {
			return fmt.Errorf("syft oci-dir:%s: %w", layout, err)
		}
	}

	return nil
}

func runImageEvidenceTrivy(ctx context.Context, trivy ImageEvidenceTrivy, out, stderr io.Writer, layout, trivyOutput, timeout string) error {
	code, err := trivy.RunInherit(ctx, out, stderr,
		"image",
		"--input", layout,
		"--scanners", "vuln",
		"--skip-version-check",
		"--timeout", timeout,
		"--format", "json",
		"--output", trivyOutput,
	)
	if err != nil {
		return fmt.Errorf("trivy image --input %s: %w", layout, err)
	}

	if code != 0 {
		return fmt.Errorf("trivy image --input %s exited with status %d: %w", layout, code, errs.ErrDependencyUnavailable)
	}

	return nil
}

func validateImageEvidenceTrivyOutput(path string) error {
	body, err := os.ReadFile(path) //nolint:gosec // caller-provided report path, same trust as CLI file flags.
	if err != nil {
		return fmt.Errorf("read trivy output %s: %w", path, err)
	}

	var report struct {
		Results json.RawMessage `json:"Results"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return fmt.Errorf("parse trivy output %s: %w: %w", path, err, errs.ErrMalformedInput)
	}

	var results any
	if len(report.Results) == 0 || json.Unmarshal(report.Results, &results) != nil {
		return fmt.Errorf("trivy output %s must be an object with a Results array: %w", path, errs.ErrMalformedInput)
	}

	if _, ok := results.([]any); !ok {
		return fmt.Errorf("trivy output %s must be an object with a Results array: %w", path, errs.ErrMalformedInput)
	}

	return nil
}

func requireImageEvidenceDir(path, label string) error {
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("%s not found: %s: %w", label, path, errs.ErrMissingInput)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory: %s: %w", label, path, errs.ErrUsage)
	}

	return nil
}

func trivyTimeoutValue(value string) string {
	if value != "" {
		return value
	}

	return defaultImageEvidenceTrivyTimeout
}

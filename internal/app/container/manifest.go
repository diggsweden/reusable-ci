// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	domaincontainer "github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// digestPattern matches an OCI image digest: lowercase `sha256:`
// prefix followed by exactly 64 lowercase hex characters. Validated
// against the docker buildx imagetools output before constructing
// the cosign-ready image-digest-ref — a docker version that changes
// its format would otherwise produce silently-broken refs that fail
// downstream signing/verification with cryptic errors.
//
//nolint:gochecknoglobals // compile-once immutable regexp.
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// DockerOps is the docker adapter surface needed by manifest helpers.
type DockerOps interface {
	Run(ctx context.Context, args ...string) (w, stderr string, err error)
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) error
}

// InspectManifestInput drives InspectManifest.
type InspectManifestInput struct {
	// Image is the exact image reference to inspect. If empty, the first
	// non-empty line from Tags is used.
	Image string
	Tags  string
}

// InspectManifestOutput is the image reference and manifest-list digest emitted
// for downstream workflow jobs.
type InspectManifestOutput struct {
	Image  string
	Digest string
}

// MergeManifestInput drives MergeManifest.
type MergeManifestInput struct {
	ImageName  string
	Tags       string
	DigestsDir string
}

// WriteDigestMarkerInput drives WriteDigestMarker.
type WriteDigestMarkerInput struct {
	Digest     string
	DigestsDir string
}

// MergeManifest creates a multi-platform manifest list from digest marker files.
func MergeManifest(ctx context.Context, docker DockerOps, w, stderr io.Writer, in MergeManifestInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	image := strings.TrimSpace(in.ImageName)
	if image == "" {
		return fmt.Errorf("image-name is required: %w", errs.ErrUsage)
	}

	dir := strings.TrimSpace(in.DigestsDir)
	if dir == "" {
		dir = domaincontainer.DefaultDigestsDir
	}

	digests, err := digestFiles(dir)
	if err != nil {
		return err
	}

	if len(digests) == 0 {
		return fmt.Errorf("no digest files found in %s: %w", dir, errs.ErrInvalidConfig)
	}

	tags := nonEmptyLines(in.Tags)
	if len(tags) == 0 {
		return fmt.Errorf("tags is required: %w", errs.ErrUsage)
	}

	args := []string{"buildx", "imagetools", "create"}
	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	for _, digest := range digests {
		args = append(args, image+"@sha256:"+digest)
	}

	return docker.RunInherit(ctx, w, stderr, args...)
}

// WriteDigestMarker creates the digest marker file consumed by MergeManifest.
func WriteDigestMarker(in WriteDigestMarkerInput) (string, error) {
	digest, err := normalizeDigest(in.Digest)
	if err != nil {
		return "", err
	}

	dir := strings.TrimSpace(in.DigestsDir)
	if dir == "" {
		dir = domaincontainer.DefaultDigestsDir
	}

	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // digest marker dir read by next step; 0755 expected.
		return "", fmt.Errorf("mkdir digest dir %s: %w", dir, err)
	}

	path := filepath.Join(dir, digest)
	if err := os.WriteFile(path, nil, 0o644); err != nil { //nolint:gosec // empty marker file; 0644 is the workflow contract.
		return "", fmt.Errorf("write digest marker %s: %w", path, err)
	}

	return path, nil
}

// InspectManifest prints `docker buildx imagetools inspect` output for the
// selected image reference, resolves the manifest digest, and emits image/digest
// CI outputs.
func InspectManifest(ctx context.Context, docker DockerOps, sink ci.OutputSink, out, stderr io.Writer, in InspectManifestInput) (*InspectManifestOutput, error) {
	image := strings.TrimSpace(in.Image)
	if image == "" {
		image = firstNonEmptyLine(in.Tags)
	}

	if image == "" {
		return nil, fmt.Errorf("image or tags is required: %w", errs.ErrUsage)
	}

	if err := docker.RunInherit(ctx, out, stderr, "buildx", "imagetools", "inspect", image); err != nil {
		return nil, err
	}

	digest, err := resolveManifestDigest(ctx, docker, stderr, image)
	if err != nil {
		return nil, err
	}

	if err := emitManifestOutputs(ctx, sink, image, digest); err != nil {
		return nil, err
	}

	return &InspectManifestOutput{Image: image, Digest: digest}, nil
}

// resolveManifestDigest runs `docker buildx imagetools inspect --format
// {{.Manifest.Digest}}` against image and returns the parsed digest.
// Validates the sha256:<64-hex> shape to catch a future buildx version
// that changes its output format.
func resolveManifestDigest(ctx context.Context, docker DockerOps, stderr io.Writer, image string) (string, error) {
	digestOut, errOut, err := docker.Run(ctx, "buildx", "imagetools", "inspect", image, "--format", "{{.Manifest.Digest}}")
	if errOut != "" && stderr != nil {
		_, _ = fmt.Fprint(stderr, errOut)
	}

	if err != nil {
		return "", err
	}

	digest := strings.TrimSpace(digestOut)
	if digest == "" {
		return "", fmt.Errorf("docker inspect returned an empty manifest digest for %s: %w", image, errs.ErrInvalidConfig)
	}

	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf(
			"docker inspect returned an unexpected digest format for %s: %q (want sha256:<64-hex>); a docker/buildx version that changed its output shape would land here: %w",
			image, digest, errs.ErrInvalidConfig,
		)
	}

	return digest, nil
}

// emitManifestOutputs writes the three outputs InspectManifest is
// responsible for. image-digest-ref is the cosign-ready immutable
// reference: the bare image name (tag stripped) joined to the
// manifest-list digest by `@`. Pre-computing here keeps the workflow
// yaml free of string-munging — `${{ steps.x.outputs.image-digest-ref
// }}` is exactly what `cosign sign` / `container sign` expects.
func emitManifestOutputs(ctx context.Context, sink ci.OutputSink, image, digest string) error {
	if err := sink.Set(ctx, "image", image); err != nil {
		return fmt.Errorf("set image: %w", err)
	}

	if err := sink.Set(ctx, "digest", digest); err != nil {
		return fmt.Errorf("set digest: %w", err)
	}

	if err := sink.Set(ctx, "image-digest-ref", stripTag(image)+"@"+digest); err != nil {
		return fmt.Errorf("set image-digest-ref: %w", err)
	}

	return nil
}

// stripTag removes the `:tag` suffix from an OCI reference, leaving
// the registry+image. Idempotent for already-bare references. Used
// to produce the canonical immutable form (`image@sha256:...`) when
// the manifest-list inspect step received a tag-ref.
//
// Naive split-on-':' would mishandle registries with explicit ports
// (e.g. `localhost:5000/img:tag`), so we look for the colon AFTER
// the last `/` — that's unambiguously the tag separator.
func stripTag(ref string) string {
	slash := strings.LastIndex(ref, "/")
	if slash < 0 {
		// No path component → registry-less reference like
		// "alpine:3.21". Strip from the colon.
		if colon := strings.LastIndex(ref, ":"); colon >= 0 {
			return ref[:colon]
		}

		return ref
	}

	tail := ref[slash+1:]

	colon := strings.LastIndex(tail, ":")
	if colon < 0 {
		return ref
	}

	return ref[:slash+1] + tail[:colon]
}

func firstNonEmptyLine(value string) string {
	for _, raw := range strings.Split(value, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			return line
		}
	}

	return ""
}

func nonEmptyLines(value string) []string {
	var out []string

	for _, raw := range strings.Split(value, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			out = append(out, line)
		}
	}

	return out
}

func digestFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read digest dir %s: %w", dir, err)
	}

	digests := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		digest, err := normalizeDigest(filepath.Base(entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("invalid digest marker %q: %w", entry.Name(), err)
		}

		digests = append(digests, digest)
	}

	sort.Strings(digests)

	return digests, nil
}

func normalizeDigest(value string) (string, error) {
	digest := strings.TrimSpace(value)

	digest = strings.TrimPrefix(digest, "sha256:")
	if len(digest) != 64 {
		return "", fmt.Errorf("digest must be sha256:<64 hex chars> or 64 hex chars: %w", errs.ErrUsage)
	}

	for _, r := range digest { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return "", fmt.Errorf("digest contains non-hex character %q: %w", r, errs.ErrUsage)
		}
	}

	return strings.ToLower(digest), nil
}

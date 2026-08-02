// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	defaultSignerImageContainerfile = "packaging/signer/Containerfile"
	defaultSignerImageContext       = "."
	defaultSignerImageLocalName     = "localhost/forgejo-ci-signer"
	defaultSignerImageAttempts      = 3
)

var (
	signerSourceSHARe = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	signerImageRefRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+@sha256:[0-9a-f]{64}$`)
)

// SignerImageTool is the buildah+skopeo surface needed by the forgejo-ci signer
// image workflow.
type SignerImageTool interface {
	BuildSignerImage(ctx context.Context, req domaincontainer.SignerImageBuildToolRequest, out io.Writer) error
	PushImage(ctx context.Context, authFile, localImage, dest string, out io.Writer) error
	RawManifest(ctx context.Context, authFile, image string, out io.Writer) ([]byte, error)
	RemoveManifest(ctx context.Context, localManifest string, out io.Writer) error
	CreateManifest(ctx context.Context, localManifest string, out io.Writer) error
	AddManifest(ctx context.Context, req domaincontainer.SignerImageManifestAddToolRequest, out io.Writer) error
	PushManifest(ctx context.Context, authFile, localManifest, dest string, out io.Writer) error
}

// SignerImageBuildArchInput drives one architecture build of the forgejo-ci
// signer image.
type SignerImageBuildArchInput struct {
	AuthFile      string
	Arch          string
	SourceSHA     string
	ServerURL     string
	Repository    string
	Containerfile string
	Context       string
	MetadataDir   string
	RetryAttempts int
	RetryDelay    time.Duration
}

// SignerImageAssembleInput drives multi-arch signer manifest assembly.
type SignerImageAssembleInput struct {
	AuthFile      string
	SourceSHA     string
	ServerURL     string
	Repository    string
	Archs         []string
	MetadataDir   string
	RetryAttempts int
	RetryDelay    time.Duration
}

// SignerImageArchMetadata is the JSON artifact uploaded by each arch job.
type SignerImageArchMetadata struct {
	Arch   string `json:"arch"`
	Tag    string `json:"tag"`
	Digest string `json:"digest"`
	Ref    string `json:"ref"`
}

// SignerImageMetadata is the JSON artifact uploaded by the manifest assembly
// job.
type SignerImageMetadata struct {
	Tag    string `json:"tag"`
	Digest string `json:"digest"`
	Ref    string `json:"ref"`
}

// BuildSignerImageArch builds and pushes one signer image architecture, writes
// signer-image-<arch>.json, and returns its metadata.
func BuildSignerImageArch(ctx context.Context, tool SignerImageTool, out io.Writer, in SignerImageBuildArchInput) (*SignerImageArchMetadata, error) {
	derived, err := deriveSignerImageArch(in)
	if err != nil {
		return nil, err
	}

	if err = recreateSignerMetadataDir(derived.MetadataDir); err != nil {
		return nil, err
	}

	if err = tool.BuildSignerImage(ctx, domaincontainer.SignerImageBuildToolRequest{
		AuthFile:      in.AuthFile,
		Platform:      derived.Platform,
		SourceURL:     derived.SourceURL,
		Revision:      in.SourceSHA,
		LocalImage:    derived.LocalImage,
		Containerfile: derived.Containerfile,
		Context:       derived.Context,
	}, out); err != nil {
		return nil, err
	}

	if err = retrySignerOperation(ctx, out, retryAttempts(in.RetryAttempts), retryDelay(in.RetryDelay), func() error {
		return tool.PushImage(ctx, in.AuthFile, derived.LocalImage, derived.ImageTag, out)
	}); err != nil {
		return nil, fmt.Errorf("failed to push signer image architecture: %w", err)
	}

	raw, err := tool.RawManifest(ctx, in.AuthFile, derived.ImageTag, out)
	if err != nil {
		return nil, err
	}

	digest := digestRawManifest(raw)
	meta := &SignerImageArchMetadata{
		Arch:   in.Arch,
		Tag:    derived.ImageTag,
		Digest: digest,
		Ref:    derived.ImageRepository + "@" + digest,
	}

	if err := writeSignerJSON(filepath.Join(derived.MetadataDir, "signer-image-"+in.Arch+".json"), meta); err != nil {
		return nil, err
	}

	_, _ = fmt.Fprintf(out, "Signer image architecture pushed: arch=%s digest=%s\n", in.Arch, digest)

	return meta, nil
}

// AssembleSignerImageManifest assembles the per-arch signer images into the
// final multi-arch manifest, writes signer-image.json, and emits CI outputs.
func AssembleSignerImageManifest(ctx context.Context, tool SignerImageTool, sink ci.OutputSink, summary ci.SummarySink, out io.Writer, in SignerImageAssembleInput) (*SignerImageMetadata, error) {
	derived, err := deriveSignerImageManifest(in)
	if err != nil {
		return nil, err
	}

	if err = recreateSignerMetadataDir(derived.MetadataDir); err != nil {
		return nil, err
	}

	if err = tool.RemoveManifest(ctx, derived.LocalManifest, out); err != nil {
		return nil, err
	}

	if err = tool.CreateManifest(ctx, derived.LocalManifest, out); err != nil {
		return nil, err
	}

	if err = addSignerArchManifests(ctx, tool, out, in.AuthFile, derived); err != nil {
		return nil, err
	}

	if err = retrySignerOperation(ctx, out, retryAttempts(in.RetryAttempts), retryDelay(in.RetryDelay), func() error {
		return tool.PushManifest(ctx, in.AuthFile, derived.LocalManifest, derived.ManifestTag, out)
	}); err != nil {
		return nil, fmt.Errorf("failed to push signer image manifest: %w", err)
	}

	raw, err := tool.RawManifest(ctx, in.AuthFile, derived.ManifestTag, out)
	if err != nil {
		return nil, err
	}

	digest := digestRawManifest(raw)
	meta := &SignerImageMetadata{
		Tag:    derived.ManifestTag,
		Digest: digest,
		Ref:    derived.ImageRepository + "@" + digest,
	}

	if err = writeSignerJSON(filepath.Join(derived.MetadataDir, "signer-image.json"), meta); err != nil {
		return nil, err
	}

	if err = emitSignerImageManifestOutputs(ctx, sink, summary, meta); err != nil {
		return nil, err
	}

	_, _ = fmt.Fprintf(out, "Signer image manifest pushed: %s\n", meta.Ref)

	return meta, nil
}

// addSignerArchManifests resolves each per-arch digest-pinned ref from its
// metadata artifact and adds it to the local manifest list.
func addSignerArchManifests(ctx context.Context, tool SignerImageTool, out io.Writer, authFile string, derived signerImageManifestDerived) error {
	for _, arch := range derived.Archs {
		ref, err := signerArchRef(arch, derived.ImageRepository)
		if err != nil {
			return err
		}

		if err := tool.AddManifest(ctx, domaincontainer.SignerImageManifestAddToolRequest{
			AuthFile:      authFile,
			Arch:          arch,
			LocalManifest: derived.LocalManifest,
			Ref:           ref,
		}, out); err != nil {
			return err
		}
	}

	return nil
}

// emitSignerImageManifestOutputs publishes the assembled manifest metadata to
// the CI output and summary sinks.
func emitSignerImageManifestOutputs(ctx context.Context, sink ci.OutputSink, summary ci.SummarySink, meta *SignerImageMetadata) error {
	if sink != nil {
		if err := sink.Set(ctx, "image-ref", meta.Ref); err != nil {
			return err
		}

		if err := sink.Set(ctx, "image-digest", meta.Digest); err != nil {
			return err
		}

		if err := sink.Set(ctx, "image-tag", meta.Tag); err != nil {
			return err
		}
	}

	if summary != nil {
		_ = summary.Append(ctx, fmt.Sprintf("### Signer image\n\n* Tag: `%s`\n* Digest: `%s`\n* Ref: `%s`\n", meta.Tag, meta.Digest, meta.Ref))
	}

	return nil
}

type signerImageArchDerived struct {
	ImageRepository string
	ImageTag        string
	LocalImage      string
	MetadataDir     string
	Platform        string
	SourceURL       string
	Containerfile   string
	Context         string
}

type signerImageManifestDerived struct {
	ImageRepository string
	ManifestTag     string
	LocalManifest   string
	MetadataDir     string
	Archs           []string
}

func deriveSignerImageArch(in SignerImageBuildArchInput) (signerImageArchDerived, error) {
	if err := validateSignerImageCommon(in.AuthFile, in.SourceSHA, in.ServerURL, in.Repository); err != nil {
		return signerImageArchDerived{}, err
	}

	if err := validateSignerArch(in.Arch); err != nil {
		return signerImageArchDerived{}, err
	}

	imageRepo := signerImageRepository(in.ServerURL, in.Repository)
	containerfile := defaultSignerString(in.Containerfile, defaultSignerImageContainerfile)
	contextDir := defaultSignerString(in.Context, defaultSignerImageContext)
	metadataDir := defaultSignerString(in.MetadataDir, "signer-image-arch-"+in.Arch)

	return signerImageArchDerived{
		ImageRepository: imageRepo,
		ImageTag:        fmt.Sprintf("%s:signer-%s-%s", imageRepo, in.SourceSHA, in.Arch),
		LocalImage:      fmt.Sprintf("%s:%s-%s", defaultSignerImageLocalName, in.SourceSHA, in.Arch),
		MetadataDir:     metadataDir,
		Platform:        "linux/" + in.Arch,
		SourceURL:       strings.TrimRight(in.ServerURL, "/") + "/" + in.Repository,
		Containerfile:   containerfile,
		Context:         contextDir,
	}, nil
}

func deriveSignerImageManifest(in SignerImageAssembleInput) (signerImageManifestDerived, error) {
	if err := validateSignerImageCommon(in.AuthFile, in.SourceSHA, in.ServerURL, in.Repository); err != nil {
		return signerImageManifestDerived{}, err
	}

	archs := in.Archs
	if len(archs) == 0 {
		archs = []string{domaincontainer.ArchAMD64, domaincontainer.ArchARM64}
	}

	for _, arch := range archs {
		if err := validateSignerArch(arch); err != nil {
			return signerImageManifestDerived{}, err
		}
	}

	imageRepo := signerImageRepository(in.ServerURL, in.Repository)

	return signerImageManifestDerived{
		ImageRepository: imageRepo,
		ManifestTag:     fmt.Sprintf("%s:signer-%s", imageRepo, in.SourceSHA),
		LocalManifest:   fmt.Sprintf("%s:manifest-%s", defaultSignerImageLocalName, in.SourceSHA),
		MetadataDir:     defaultSignerString(in.MetadataDir, "signer-image-dist"),
		Archs:           archs,
	}, nil
}

func validateSignerImageCommon(authFile, sourceSHA, serverURL, repository string) error {
	if err := requireSingleLine(authFile, "auth-file"); err != nil {
		return err
	}

	if _, err := os.Stat(authFile); err != nil {
		return fmt.Errorf("auth-file not found: %s: %w", authFile, err)
	}

	if !signerSourceSHARe.MatchString(sourceSHA) {
		return fmt.Errorf("SOURCE_SHA must be a git commit hex digest: %w", errs.ErrValidation)
	}

	if err := requireSingleLine(serverURL, "server-url"); err != nil {
		return err
	}

	return requireSingleLine(repository, "repository")
}

func validateSignerArch(arch string) error {
	switch arch {
	case domaincontainer.ArchAMD64, domaincontainer.ArchARM64:
		return nil
	default:
		return fmt.Errorf("unsupported signer architecture: %s: %w", arch, errs.ErrValidation)
	}
}

func signerImageRepository(serverURL, repository string) string {
	registryHost := strings.TrimPrefix(strings.TrimPrefix(serverURL, "https://"), "http://")

	return registryHost + "/" + strings.ToLower(repository) + "-signer"
}

func signerArchRef(arch, imageRepository string) (string, error) {
	metadataFile := filepath.Join("signer-image-arch-"+arch, "signer-image-"+arch+".json")

	body, err := os.ReadFile(metadataFile) //nolint:gosec // workspace-local metadata artifact downloaded by the workflow.
	if err != nil {
		return "", fmt.Errorf("signer image metadata missing: %s: %w", metadataFile, err)
	}

	var meta SignerImageArchMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("parse signer image metadata %s: %w: %w", metadataFile, err, errs.ErrInvalidConfig)
	}

	ref := meta.Ref
	if !signerImageRefRe.MatchString(ref) {
		return "", fmt.Errorf("signer image ref must be digest-pinned: %s: %w", ref, errs.ErrValidation)
	}

	if !strings.HasPrefix(ref, imageRepository+"@sha256:") {
		return "", fmt.Errorf("signer image ref must be under %s: %s: %w", imageRepository, ref, errs.ErrValidation)
	}

	return ref, nil
}

func retrySignerOperation(ctx context.Context, out io.Writer, attempts int, delay time.Duration, fn func() error) error {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}

		if attempt == attempts {
			break
		}

		wait := time.Duration(attempt) * delay
		if out != nil {
			_, _ = fmt.Fprintf(out, "Command failed (attempt %d/%d), retrying in %s...\n", attempt, attempts, wait)
		}

		if wait <= 0 {
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return ctx.Err()
		case <-timer.C:
		}
	}

	return err
}

func retryAttempts(value int) int {
	if value > 0 {
		return value
	}

	return defaultSignerImageAttempts
}

func retryDelay(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}

	return 10 * time.Second
}

func digestRawManifest(raw []byte) string {
	sum := sha256.Sum256(raw)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeSignerJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal signer image metadata: %w", err)
	}

	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil { //nolint:gosec,mnd // public CI metadata artifact.
		return fmt.Errorf("write signer image metadata %s: %w", path, err)
	}

	return nil
}

func recreateSignerMetadataDir(path string) error {
	if path == "" || path == "." || path == "/" || filepath.IsAbs(path) || strings.ContainsAny(path, "\n\r") || strings.Contains(path, "..") {
		return fmt.Errorf("metadata-dir must be a safe relative directory: %s: %w", path, errs.ErrUsage)
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove signer image metadata dir %s: %w", path, err)
	}

	if err := os.MkdirAll(path, 0o755); err != nil { //nolint:gosec,mnd // public CI metadata artifact dir.
		return fmt.Errorf("create signer image metadata dir %s: %w", path, err)
	}

	return nil
}

func requireSingleLine(value, label string) error {
	if value == "" || strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("%s must be a non-empty single-line value: %w", label, errs.ErrUsage)
	}

	return nil
}

func defaultSignerString(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

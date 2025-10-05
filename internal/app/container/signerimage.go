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
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

const (
	// defaultMultiarchImageContainerfile and defaultMultiarchImageName are the
	// forge/consumer-neutral defaults; a caller (e.g. a signer image) overrides
	// them with its own Containerfile path and metadata basename.
	defaultMultiarchImageContainerfile = "Containerfile"
	defaultMultiarchImageName          = "image"
	defaultSignerImageContext          = "."
	defaultSignerImageAttempts         = 3
	defaultSignerImageRetryDelay       = 10 * time.Second
)

// SignerImageTool is the buildah+skopeo surface needed by the signer image
// workflow.
type SignerImageTool interface {
	BuildSignerImage(ctx context.Context, req domaincontainer.SignerImageBuildToolRequest, out io.Writer) error
	PushImage(ctx context.Context, authFile, localImage, dest string, out io.Writer) error
	RawManifest(ctx context.Context, authFile, image string, out io.Writer) ([]byte, error)
	RemoveManifest(ctx context.Context, localManifest string, out io.Writer) error
	CreateManifest(ctx context.Context, localManifest string, out io.Writer) error
	AddManifest(ctx context.Context, req domaincontainer.SignerImageManifestAddToolRequest, out io.Writer) error
	PushManifest(ctx context.Context, authFile, localManifest, dest string, out io.Writer) error
}

// SignerImageBuildArchInput drives one architecture build of the signer image.
type SignerImageBuildArchInput struct {
	AuthFile         string
	Arch             string
	SourceSHA        string
	ServerURL        string
	Repository       string
	RepositorySuffix string // appended to lower(repository) to form the image repo (e.g. "-signer")
	TagPrefix        string // prepended to the source-SHA image tag (e.g. "signer-")
	Name             string // metadata basename shared with assemble; empty defaults to "image"
	Title            string // org.opencontainers.image.title label; empty omits the label
	Containerfile    string
	Context          string
	MetadataDir      string
	RetryAttempts    int
	RetryDelay       time.Duration
}

// SignerImageAssembleInput drives multi-arch manifest assembly.
type SignerImageAssembleInput struct {
	AuthFile         string
	SourceSHA        string
	ServerURL        string
	Repository       string
	RepositorySuffix string
	TagPrefix        string
	Name             string
	Archs            []string
	MetadataDir      string
	RetryAttempts    int
	RetryDelay       time.Duration
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
		Title:         in.Title,
	}, out); err != nil {
		return nil, err
	}

	if err = retry.Run(ctx, out, retry.Attempts(in.RetryAttempts, defaultSignerImageAttempts), retry.Delay(in.RetryDelay, defaultSignerImageRetryDelay), func() error {
		return tool.PushImage(ctx, in.AuthFile, derived.LocalImage, derived.ImageTag, out)
	}); err != nil {
		return nil, fmt.Errorf("failed to push signer image architecture: %w", err)
	}

	raw, err := tool.RawManifest(ctx, in.AuthFile, derived.ImageTag, out)
	if err != nil {
		return nil, err
	}

	digest, err := digestRawManifest(raw, derived.ImageTag)
	if err != nil {
		return nil, err
	}

	meta := &SignerImageArchMetadata{
		Arch:   in.Arch,
		Tag:    derived.ImageTag,
		Digest: digest,
		Ref:    derived.ImageRepository + "@" + digest,
	}

	if err := writeSignerJSON(filepath.Join(derived.MetadataDir, derived.Name+"-"+in.Arch+".json"), meta); err != nil {
		return nil, err
	}

	_, _ = fmt.Fprintf(out, "Image architecture pushed: arch=%s digest=%s\n", in.Arch, digest)

	return meta, nil
}

// AssembleSignerImageManifest assembles the per-arch signer images into the
// final multi-arch manifest, writes signer-image.json, and emits CI outputs.
func AssembleSignerImageManifest(ctx context.Context, tool SignerImageTool, sink ci.OutputSink, summary ci.SummarySink, out io.Writer, in SignerImageAssembleInput) (*SignerImageMetadata, error) { //nolint:cyclop // complete metadata preflight precedes the linear index/push/output phases.
	derived, err := deriveSignerImageManifest(in)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(derived.Archs))
	for _, arch := range derived.Archs {
		ref, refErr := signerArchRef(arch, derived.ImageRepository, derived.Name, derived.ManifestTag+"-"+arch)
		if refErr != nil {
			return nil, refErr
		}

		if seen[ref] {
			return nil, fmt.Errorf("signer architectures must identify distinct image digests: %w", errs.ErrValidation)
		}

		seen[ref] = true
		derived.Refs = append(derived.Refs, ref)
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

	if err = retry.Run(ctx, out, retry.Attempts(in.RetryAttempts, defaultSignerImageAttempts), retry.Delay(in.RetryDelay, defaultSignerImageRetryDelay), func() error {
		return tool.PushManifest(ctx, in.AuthFile, derived.LocalManifest, derived.ManifestTag, out)
	}); err != nil {
		return nil, fmt.Errorf("failed to push signer image manifest: %w", err)
	}

	raw, err := tool.RawManifest(ctx, in.AuthFile, derived.ManifestTag, out)
	if err != nil {
		return nil, err
	}

	digest, err := digestRawManifest(raw, derived.ManifestTag)
	if err != nil {
		return nil, err
	}

	meta := &SignerImageMetadata{
		Tag:    derived.ManifestTag,
		Digest: digest,
		Ref:    derived.ImageRepository + "@" + digest,
	}

	if err = writeSignerJSON(filepath.Join(derived.MetadataDir, derived.Name+".json"), meta); err != nil {
		return nil, err
	}

	if err = emitSignerImageManifestOutputs(ctx, sink, summary, meta); err != nil {
		return nil, err
	}

	_, _ = fmt.Fprintf(out, "Image manifest pushed: %s\n", meta.Ref)

	return meta, nil
}

// addSignerArchManifests adds the prevalidated per-architecture references.
func addSignerArchManifests(ctx context.Context, tool SignerImageTool, out io.Writer, authFile string, derived signerImageManifestDerived) error {
	for index, arch := range derived.Archs {
		if err := tool.AddManifest(ctx, domaincontainer.SignerImageManifestAddToolRequest{
			AuthFile:      authFile,
			Arch:          arch,
			LocalManifest: derived.LocalManifest,
			Ref:           derived.Refs[index],
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
		if err := summary.Append(ctx, fmt.Sprintf("### Image\n\n* Tag: `%s`\n* Digest: `%s`\n* Ref: `%s`\n", meta.Tag, meta.Digest, meta.Ref)); err != nil {
			return fmt.Errorf("append signer image summary: %w", err)
		}
	}

	return nil
}

type signerImageArchDerived struct {
	ImageRepository string
	ImageTag        string
	LocalImage      string
	MetadataDir     string
	Name            string
	Platform        string
	SourceURL       string
	Containerfile   string
	Context         string
}

type signerImageManifestDerived struct {
	ImageRepository string
	ManifestTag     string
	Name            string
	LocalManifest   string
	MetadataDir     string
	Archs           []string
	Refs            []string
}

func deriveSignerImageArch(in SignerImageBuildArchInput) (signerImageArchDerived, error) {
	if err := validateSignerMetadataName(in.Name); err != nil {
		return signerImageArchDerived{}, err
	}

	if err := validateSignerImageCommon(in.AuthFile, in.SourceSHA, in.ServerURL, in.Repository); err != nil {
		return signerImageArchDerived{}, err
	}

	if err := validateSignerArch(in.Arch); err != nil {
		return signerImageArchDerived{}, err
	}

	imageRepo := signerImageRepository(in.ServerURL, in.Repository, in.RepositorySuffix)
	name := defaultSignerString(in.Name, defaultMultiarchImageName)
	containerfile := defaultSignerString(in.Containerfile, defaultMultiarchImageContainerfile)
	contextDir := defaultSignerString(in.Context, defaultSignerImageContext)
	metadataDir := defaultSignerString(in.MetadataDir, name+"-arch-"+in.Arch)

	return signerImageArchDerived{
		ImageRepository: imageRepo,
		ImageTag:        fmt.Sprintf("%s:%s%s-%s", imageRepo, in.TagPrefix, in.SourceSHA, in.Arch),
		LocalImage:      fmt.Sprintf("%s:%s-%s", localScratchImage(name), in.SourceSHA, in.Arch),
		MetadataDir:     metadataDir,
		Name:            name,
		Platform:        "linux/" + in.Arch,
		SourceURL:       strings.TrimRight(in.ServerURL, "/") + "/" + in.Repository,
		Containerfile:   containerfile,
		Context:         contextDir,
	}, nil
}

func deriveSignerImageManifest(in SignerImageAssembleInput) (signerImageManifestDerived, error) {
	if err := validateSignerMetadataName(in.Name); err != nil {
		return signerImageManifestDerived{}, err
	}

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

	imageRepo := signerImageRepository(in.ServerURL, in.Repository, in.RepositorySuffix)
	name := defaultSignerString(in.Name, defaultMultiarchImageName)

	return signerImageManifestDerived{
		ImageRepository: imageRepo,
		ManifestTag:     fmt.Sprintf("%s:%s%s", imageRepo, in.TagPrefix, in.SourceSHA),
		LocalManifest:   fmt.Sprintf("%s:manifest-%s", localScratchImage(name), in.SourceSHA),
		MetadataDir:     defaultSignerString(in.MetadataDir, name+"-dist"),
		Name:            name,
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

	if !git.ValidCommitSHA(sourceSHA) {
		return fmt.Errorf("SOURCE_SHA must be a git commit hex digest: %w", errs.ErrValidation)
	}

	if err := requireSingleLine(serverURL, "server-url"); err != nil {
		return err
	}

	return requireSingleLine(repository, "repository")
}

func validateSignerMetadataName(name string) error {
	if name != "" && (name == "." || !pathsafe.Relative(name) || filepath.Base(name) != name || strings.ContainsAny(name, "\\\r\n")) {
		return fmt.Errorf("metadata name must be a plain basename: %w", errs.ErrUsage)
	}

	return nil
}

func validateSignerArch(arch string) error {
	switch arch {
	case domaincontainer.ArchAMD64, domaincontainer.ArchARM64:
		return nil
	default:
		return fmt.Errorf("unsupported signer architecture: %s: %w", arch, errs.ErrValidation)
	}
}

func signerImageRepository(serverURL, repository, suffix string) string {
	registryHost := strings.TrimPrefix(strings.TrimPrefix(serverURL, "https://"), "http://")

	return registryHost + "/" + strings.ToLower(repository) + suffix
}

// localScratchImage is the throwaway local buildah tag used while building and
// assembling; it never leaves the runner. It is derived from the metadata name
// so a non-signer multiarch build isn't tagged "signer".
func localScratchImage(name string) string {
	return "localhost/" + name
}

func signerArchRef(arch, imageRepository, name, expectedTag string) (string, error) {
	metadataFile := filepath.Join(name+"-arch-"+arch, name+"-"+arch+".json")

	body, err := os.ReadFile(metadataFile) //nolint:gosec // workspace-local metadata artifact downloaded by the workflow.
	if err != nil {
		return "", fmt.Errorf("image metadata missing: %s: %w", metadataFile, err)
	}

	var meta SignerImageArchMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("parse image metadata %s: %w: %w", metadataFile, err, errs.ErrInvalidConfig)
	}

	ref := meta.Ref
	if !domaincontainer.ValidDigestPinnedRef(ref) {
		return "", fmt.Errorf("image ref must be digest-pinned: %s: %w", ref, errs.ErrValidation)
	}

	if !strings.HasPrefix(ref, imageRepository+"@sha256:") {
		return "", fmt.Errorf("image ref must be under %s: %s: %w", imageRepository, ref, errs.ErrValidation)
	}

	if meta.Arch != arch || meta.Tag != expectedTag || !domaincontainer.ValidDigest(meta.Digest) || ref != imageRepository+"@"+meta.Digest {
		return "", fmt.Errorf("image metadata architecture, tag, digest and ref must agree: %w", errs.ErrValidation)
	}

	return ref, nil
}

// digestRawManifest is the content digest of the manifest the registry
// served. The digest names what every later step signs and verifies, so a
// body that cannot be a manifest -- empty, or not a JSON object -- is refused
// rather than hashed into a reference to nothing.
func digestRawManifest(raw []byte, image string) (string, error) {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest == nil {
		return "", fmt.Errorf("registry returned no manifest document for %s: %w", image, errs.ErrMalformedInput)
	}

	sum := sha256.Sum256(raw)

	return "sha256:" + hex.EncodeToString(sum[:]), nil
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
	if !pathsafe.Relative(path) || filepath.Clean(path) == "." || strings.Contains(path, "\\") {
		return fmt.Errorf("metadata-dir must be a safe relative directory: %s: %w", path, errs.ErrUsage)
	}

	root, err := pathsafe.MkdirRoot(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	base := filepath.Base(path)
	if info, err := root.Lstat(base); err == nil && !info.IsDir() {
		return fmt.Errorf("metadata directory must be a real directory: %w", errs.ErrValidation)
	}

	if err := root.RemoveAll(base); err != nil {
		return fmt.Errorf("remove signer image metadata dir %s: %w", path, err)
	}

	if err := root.Mkdir(base, 0o755); err != nil {
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

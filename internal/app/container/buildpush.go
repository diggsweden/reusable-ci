// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

const (
	defaultBuildPushContext       = "."
	defaultBuildPushRetryAttempts = 3
	defaultBuildPushRetryDelay    = 15 * time.Second

	// tls-verify flag values shared by the push commands.
	tlsVerifyTrue  = "true"
	tlsVerifyFalse = "false"

	// outputKeyDigest is the OutputSink key the push/build commands publish
	// the image digest under.
	outputKeyDigest = "digest"
)

// BuildPushTool is the Buildah surface needed by build-push-oci-image.
type BuildPushTool interface {
	BuildManifest(ctx context.Context, req domaincontainer.BuildPushManifestBuildRequest, out io.Writer) error
	PushManifestWithDigest(ctx context.Context, authFile string, tlsVerify bool, manifest string, out io.Writer) (string, error)
}

// BuildPushGit reads checkout identity/timestamp for reproducible labels.
type BuildPushGit interface {
	RevParse(ctx context.Context, ref string) (string, error)
	CommitUnixTime(ctx context.Context, ref string) (string, error)
}

// BuildPushOCIImageInput drives `container build-push-oci-image`, a Go port of
// forgejo-ci's public build-push-oci-image action body.
type BuildPushOCIImageInput struct {
	Tag           string
	Containerfile string
	BuildsJSON    string
	Image         string
	Context       string

	// OCILabels carries the caller-supplied org.opencontainers.image.*
	// label fields; RefName, Version, Revision, and Documentation default
	// from the tag/checkout when empty.
	domaincontainer.OCILabels

	AuthFile      string
	TLSVerify     string
	ServerURL     string
	Repository    string
	RetryAttempts int
	RetryDelay    time.Duration
}

// BuildPushOCIImageOutput is the action output contract.
type BuildPushOCIImageOutput struct {
	Image  string
	Digest string
}

// BuildPushOCIImage builds every requested platform into one buildah manifest,
// pushes it, and writes image/digest outputs.
func BuildPushOCIImage(ctx context.Context, tool BuildPushTool, git BuildPushGit, sink ci.OutputSink, out io.Writer, in BuildPushOCIImageInput) (*BuildPushOCIImageOutput, error) {
	plan, err := deriveBuildPushPlan(ctx, git, in)
	if err != nil {
		return nil, err
	}

	if buildErr := buildPushPlatforms(ctx, tool, out, in, plan); buildErr != nil {
		return nil, buildErr
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "Pushing multi-arch manifest %s...\n", plan.Manifest)
	}

	digest, err := retry.Do(ctx, out, retry.Attempts(in.RetryAttempts, defaultBuildPushRetryAttempts), retry.Delay(in.RetryDelay, defaultBuildPushRetryDelay), func() (string, error) {
		return tool.PushManifestWithDigest(ctx, in.AuthFile, plan.TLSVerify, plan.Manifest, out)
	})
	if err != nil {
		return nil, err
	}

	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, fmt.Errorf("buildah manifest push produced an empty digest: %w", errs.ErrDependencyUnavailable)
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "Pushed %s@%s\n", plan.Manifest, digest)
	}

	if sink != nil {
		if err := sink.Set(ctx, "image", plan.Image); err != nil {
			return nil, err
		}

		if err := sink.Set(ctx, outputKeyDigest, digest); err != nil {
			return nil, err
		}
	}

	return &BuildPushOCIImageOutput{Image: plan.Image, Digest: digest}, nil
}

// buildPushPlatforms builds every planned platform into the shared manifest.
func buildPushPlatforms(ctx context.Context, tool BuildPushTool, out io.Writer, in BuildPushOCIImageInput, plan buildPushPlan) error {
	for _, build := range plan.Builds {
		if out != nil {
			_, _ = fmt.Fprintf(out, "Building %s image for %s...\n", build.Platform, plan.Manifest)
		}

		if err := tool.BuildManifest(ctx, domaincontainer.BuildPushManifestBuildRequest{
			AuthFile:        in.AuthFile,
			Manifest:        plan.Manifest,
			TLSVerify:       plan.TLSVerify,
			SourceDateEpoch: plan.SourceDateEpoch,
			Platform:        build.Platform,
			Containerfile:   in.Containerfile,
			BuildArgs:       build.BuildArgs,
			Labels:          plan.Labels,
			Context:         plan.Context,
		}, out); err != nil {
			return err
		}
	}

	return nil
}

type buildPushPlan struct {
	Image           string
	Manifest        string
	Context         string
	TLSVerify       bool
	Builds          []domaincontainer.BuildPushPlatformBuild
	Labels          []string
	SourceDateEpoch string
}

func deriveBuildPushPlan(ctx context.Context, git BuildPushGit, in BuildPushOCIImageInput) (buildPushPlan, error) {
	if err := validateBuildPushInput(in); err != nil {
		return buildPushPlan{}, err
	}

	builds, err := parseBuildPushBuilds(in.BuildsJSON)
	if err != nil {
		return buildPushPlan{}, err
	}

	tlsVerify := in.TLSVerify == tlsVerifyTrue

	image := in.Image
	if image == "" {
		image = defaultBuildPushImage(in.ServerURL, in.Repository)
	}

	url := strings.TrimRight(in.ServerURL, "/") + "/" + in.Repository
	refName := defaultBuildPushString(in.RefName, in.Tag)
	version := defaultBuildPushString(in.Version, in.Tag)
	documentation := defaultBuildPushString(in.Documentation, url+"#readme")

	revision := in.Revision
	if revision == "" {
		revision, err = git.RevParse(ctx, "HEAD")
		if err != nil {
			return buildPushPlan{}, err
		}
	}

	sourceDateEpoch, err := git.CommitUnixTime(ctx, "HEAD")
	if err != nil {
		return buildPushPlan{}, err
	}

	created, err := buildPushCreated(sourceDateEpoch)
	if err != nil {
		return buildPushPlan{}, err
	}

	// Start from the caller-supplied labels and fill in the derived values.
	ociLabels := in.OCILabels
	ociLabels.Version = version
	ociLabels.Revision = revision
	ociLabels.RefName = refName
	ociLabels.Documentation = documentation

	labels, err := OCIReleaseLabels(OCIReleaseLabelsInput{
		OCILabels: ociLabels,
		Created:   created,
		Source:    url,
	})
	if err != nil {
		return buildPushPlan{}, err
	}

	return buildPushPlan{
		Image:           image,
		Manifest:        image + ":" + in.Tag,
		Context:         defaultBuildPushString(in.Context, defaultBuildPushContext),
		TLSVerify:       tlsVerify,
		Builds:          builds,
		Labels:          labels,
		SourceDateEpoch: sourceDateEpoch,
	}, nil
}

func validateBuildPushInput(in BuildPushOCIImageInput) error {
	if err := validateBuildPushSources(in); err != nil {
		return err
	}

	return validateBuildPushRegistry(in)
}

// validateBuildPushSources checks the tag and Containerfile inputs.
func validateBuildPushSources(in BuildPushOCIImageInput) error {
	if in.Tag == "" || strings.ContainsAny(in.Tag, " \n") {
		return fmt.Errorf("invalid tag: %s: %w", in.Tag, errs.ErrUsage)
	}

	if in.Containerfile == "" {
		return fmt.Errorf("containerfile is required: %w", errs.ErrUsage)
	}

	info, err := os.Stat(in.Containerfile)
	if err != nil || info.IsDir() {
		return fmt.Errorf("containerfile not found: %s: %w", in.Containerfile, errs.ErrMissingInput)
	}

	return nil
}

// validateBuildPushRegistry checks TLS, auth-file, and registry target inputs.
func validateBuildPushRegistry(in BuildPushOCIImageInput) error {
	switch in.TLSVerify {
	case tlsVerifyTrue, tlsVerifyFalse:
	default:
		return fmt.Errorf("tls-verify must be 'true' or 'false', got %q: %w", in.TLSVerify, errs.ErrUsage)
	}

	if in.AuthFile != "" {
		info, err := os.Stat(in.AuthFile)
		if err != nil || info.IsDir() {
			return fmt.Errorf("auth-file not found: %s: %w", in.AuthFile, errs.ErrMissingInput)
		}
	}

	if in.ServerURL == "" || in.Repository == "" {
		return fmt.Errorf("server-url and repository are required: %w", errs.ErrUsage)
	}

	return nil
}

func parseBuildPushBuilds(raw string) ([]domaincontainer.BuildPushPlatformBuild, error) {
	var builds []domaincontainer.BuildPushPlatformBuild
	if err := json.Unmarshal([]byte(raw), &builds); err != nil {
		return nil, fmt.Errorf("parse builds JSON: %w: %w", err, errs.ErrInvalidConfig)
	}

	if len(builds) == 0 {
		return nil, fmt.Errorf("builds must be a non-empty JSON array: %w", errs.ErrUsage)
	}

	for _, build := range builds {
		switch {
		case build.Platform == "":
			return nil, fmt.Errorf("build platform is required: %w", errs.ErrUsage)
		case !strings.HasPrefix(build.Platform, "linux/"):
			return nil, fmt.Errorf("unsupported platform: %s: %w", build.Platform, errs.ErrUsage)
		}
	}

	return builds, nil
}

func buildPushCreated(epoch string) (string, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(epoch), 10, 64)
	if err != nil {
		return "", fmt.Errorf("git commit timestamp must be unix seconds, got %q: %w", epoch, errs.ErrInvalidConfig)
	}

	return time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05Z"), nil
}

func defaultBuildPushImage(serverURL, repository string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(serverURL, "http://"), "https://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}

	return host + "/" + strings.ToLower(repository)
}

func defaultBuildPushString(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// RawManifestRegistry fetches the manifest JSON served for an image reference.
type RawManifestRegistry interface {
	Manifest(ctx context.Context, ref string) ([]byte, error)
}

// ContainerfileArgDefaultInput drives ContainerfileArgDefault.
type ContainerfileArgDefaultInput struct {
	File string
	Name string
}

// PlatformRefInput drives PlatformRef.
type PlatformRefInput struct {
	Ref      string
	Platform string
}

// ContainerfileArgDefault returns the default value for ARG <Name>=... before
// the first FROM. This is intentionally the same narrow parser the old shell
// helper used for base-image defaults, not a full Dockerfile frontend.
func ContainerfileArgDefault(in ContainerfileArgDefaultInput) (string, error) {
	if in.File == "" || in.Name == "" {
		return "", fmt.Errorf("file and name are required: %w", errs.ErrUsage)
	}

	file, err := os.Open(in.File) //nolint:gosec // caller-selected Containerfile path.
	if err != nil {
		return "", fmt.Errorf("open containerfile %s: %w: %w", in.File, err, errs.ErrMissingInput)
	}

	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)

	value, found := scanContainerfileArgDefault(scanner, in.Name)
	if found {
		return value, nil
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read containerfile %s: %w", in.File, err)
	}

	return "", fmt.Errorf("%s lacks ARG %s=... before first FROM: %w", in.File, in.Name, errs.ErrValidation)
}

// scanContainerfileArgDefault scans lines before the first FROM for a
// non-empty ARG <name>=<value> default.
func scanContainerfileArgDefault(scanner *bufio.Scanner, argName string) (string, bool) {
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "FROM ") || strings.HasPrefix(line, "from ") {
			break
		}

		if !strings.HasPrefix(line, "ARG ") {
			continue
		}

		line = strings.TrimPrefix(line, "ARG ")

		name, value, ok := strings.Cut(line, "=")
		if ok && name == argName && value != "" {
			return value, true
		}
	}

	return "", false
}

// CanonicalRef canonicalizes a Docker image reference.
func CanonicalRef(ref string) (string, error) {
	return domaincontainer.CanonicalImageRef(ref)
}

// ImageNameForRef returns the repository/name for a Docker image reference.
func ImageNameForRef(ref string) (string, error) {
	return domaincontainer.ImageNameForRef(ref)
}

// PlatformRef resolves ref to the single-platform digest ref Buildah should use
// for platform. Non-index manifests return the canonical ref unchanged.
func PlatformRef(ctx context.Context, registry RawManifestRegistry, in PlatformRefInput) (string, error) {
	if registry == nil {
		return "", fmt.Errorf("registry is required: %w", errs.ErrUsage)
	}

	wanted, err := parsePlatform(in.Platform)
	if err != nil {
		return "", err
	}

	inspectRef, err := domaincontainer.CanonicalImageRef(in.Ref)
	if err != nil {
		return "", err
	}

	raw, err := registry.Manifest(ctx, inspectRef)
	if err != nil {
		return "", err
	}

	digest, isIndex, err := platformDigest(raw, wanted)
	if err != nil {
		return "", err
	}

	if !isIndex {
		return inspectRef, nil
	}

	if digest == "" {
		return "", fmt.Errorf("image %s lacks platform %s: %w", in.Ref, in.Platform, errs.ErrValidation)
	}

	imageName, err := domaincontainer.ImageNameForRef(inspectRef)
	if err != nil {
		return "", err
	}

	return imageName + "@" + digest, nil
}

type platformParts struct {
	os           string
	architecture string
	variant      string
}

func parsePlatform(platform string) (platformParts, error) {
	parts := strings.Split(platform, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return platformParts{}, fmt.Errorf("platform must be os/arch or os/arch/variant: %s: %w", platform, errs.ErrUsage)
	}

	out := platformParts{os: parts[0], architecture: parts[1]}
	if len(parts) == 3 {
		out.variant = parts[2]
	}

	return out, nil
}

func platformDigest(raw []byte, wanted platformParts) (string, bool, error) {
	var doc struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
				Variant      string `json:"variant"`
			} `json:"platform"`
		} `json:"manifests"`
	}

	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false, fmt.Errorf("parse image manifest JSON: %w: %w", err, errs.ErrMalformedInput)
	}

	if doc.Manifests == nil {
		return "", false, nil
	}

	for _, manifest := range doc.Manifests {
		if manifest.Platform.OS != wanted.os || manifest.Platform.Architecture != wanted.architecture {
			continue
		}

		if wanted.variant != "" && manifest.Platform.Variant != wanted.variant {
			continue
		}

		return manifest.Digest, true, nil
	}

	return "", true, nil
}

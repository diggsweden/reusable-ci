// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// releaseRequestGit is the git surface VerifyReleaseRequest needs. Remote
// methods query origin at the trust boundary; local methods inspect the checked
// out repository after fetching the immutable request tag when needed.
type releaseRequestGit interface {
	RevParse(ctx context.Context, ref string) (string, error)
	FetchTagFromRemote(ctx context.Context, remote, tag string) error
	RemoteTagObject(ctx context.Context, remote, tag string) (string, error)
	RemoteTagExists(ctx context.Context, remote, tag string) (bool, error)
	CatFileType(ctx context.Context, ref string) (string, error)
	VerifyTagSSHAgainstAllowedSigners(ctx context.Context, tag, allowedSignersPath string) (ok bool, output string, err error)
}

// VerifyReleaseRequestInput drives VerifyReleaseRequest.
type VerifyReleaseRequestInput struct {
	ReleaseRequest     string // release-request/vMAJOR.MINOR.PATCH
	ReleaseTag         string // vMAJOR.MINOR.PATCH
	AllowedSignersPath string // OpenSSH allowed_signers file
	Remote             string // empty defaults to origin
}

// VerifyReleaseRequest verifies the signed human authorisation tag that asks CI
// to create a final immutable release tag. It is the Go port of
// forgejo-ci's verify-release-request.sh and preserves that boundary:
// stable request/tag names only, local request object must match origin, the
// final tag must not already exist, and the request tag must be annotated and
// SSH-signed by an allowed signer.
//
//nolint:cyclop // mirrors a linear trust-boundary shell script; splitting would hide the contract.
func VerifyReleaseRequest(ctx context.Context, git releaseRequestGit, out io.Writer, in VerifyReleaseRequestInput) error {
	if in.ReleaseRequest == "" || in.ReleaseTag == "" || in.AllowedSignersPath == "" {
		return fmt.Errorf("verify-release-request: release-request, tag, and allowed-signers-file are required: %w", errs.ErrUsage)
	}

	if hasLineBreak(in.ReleaseRequest) || hasLineBreak(in.ReleaseTag) || hasLineBreak(in.AllowedSignersPath) {
		return fmt.Errorf("verify-release-request: release request inputs must be single-line values: %w", errs.ErrValidation)
	}

	requestTag, ok := version.ReleaseRequestVersion(in.ReleaseRequest)
	if !ok || in.ReleaseRequest != version.ReleaseRequestPrefix+requestTag || !version.IsStableSemverTag(requestTag) {
		return fmt.Errorf("verify-release-request: release request must look like release-request/vMAJOR.MINOR.PATCH: %s: %w", in.ReleaseRequest, errs.ErrValidation)
	}

	if !version.IsStableSemverTag(in.ReleaseTag) {
		return fmt.Errorf("verify-release-request: release tag must look like stable vMAJOR.MINOR.PATCH: %s: %w", in.ReleaseTag, errs.ErrValidation)
	}

	if requestTag != in.ReleaseTag {
		return fmt.Errorf("verify-release-request: release request %s does not authorize %s: %w", in.ReleaseRequest, in.ReleaseTag, errs.ErrValidation)
	}

	if err := requireNonEmptyFile(in.AllowedSignersPath); err != nil {
		return err
	}

	remote := in.Remote
	if remote == "" {
		remote = "origin"
	}

	requestRef := "refs/tags/" + in.ReleaseRequest
	if _, err := git.RevParse(ctx, requestRef); err != nil {
		if err := git.FetchTagFromRemote(ctx, remote, in.ReleaseRequest); err != nil {
			return fmt.Errorf("verify-release-request: fetch release request tag: %w", err)
		}
	}

	localRequestObject, err := git.RevParse(ctx, requestRef)
	if err != nil {
		return fmt.Errorf("verify-release-request: resolve local release request tag: %w", err)
	}

	remoteRequestObject, err := git.RemoteTagObject(ctx, remote, in.ReleaseRequest)
	if err != nil {
		return fmt.Errorf("verify-release-request: resolve origin release request tag: %w", err)
	}

	if localRequestObject != remoteRequestObject {
		return fmt.Errorf("verify-release-request: local release request tag object does not match origin\n  local:  %s\n  origin: %s: %w", localRequestObject, remoteRequestObject, errs.ErrValidation)
	}

	objType, err := git.CatFileType(ctx, requestRef)
	if err != nil {
		return fmt.Errorf("verify-release-request: inspect release request tag object: %w", err)
	}

	if objType != "tag" {
		return fmt.Errorf("verify-release-request: release request must be an annotated signed tag: %s: %w", in.ReleaseRequest, errs.ErrValidation)
	}

	finalExists, err := git.RemoteTagExists(ctx, remote, in.ReleaseTag)
	if err != nil {
		return fmt.Errorf("verify-release-request: check final release tag on origin: %w", err)
	}

	if finalExists {
		return fmt.Errorf("verify-release-request: final release tag already exists on origin; refusing to recreate it: %s: %w", in.ReleaseTag, errs.ErrValidation)
	}

	verified, verifyOutput, err := git.VerifyTagSSHAgainstAllowedSigners(ctx, in.ReleaseRequest, in.AllowedSignersPath)
	if err != nil {
		return fmt.Errorf("verify-release-request: verify release request SSH signature: %w", err)
	}

	if !verified {
		return fmt.Errorf("verify-release-request: SSH signature rejected by allowed_signers: %s: %w", strings.TrimSpace(verifyOutput), errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "Release request signature verified: %s -> %s\n", in.ReleaseRequest, in.ReleaseTag)

	return nil
}

func hasLineBreak(value string) bool {
	return strings.ContainsAny(value, "\n\r")
}

func requireNonEmptyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("verify-release-request: release request SSH allowed signers file is missing or empty: %s: %w", path, errs.ErrValidation)
	}

	if info.Size() == 0 {
		return fmt.Errorf("verify-release-request: release request SSH allowed signers file is missing or empty: %s: %w", path, errs.ErrValidation)
	}

	return nil
}

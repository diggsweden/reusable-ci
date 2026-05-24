// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package ci

import (
	"context"
	"fmt"
	"io"
	"strings"

	domainci "github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// GitOps is the git operation subset needed by ResolveRef.
type GitOps interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// ResolveRefInput drives `ci resolve-ref`.
type ResolveRefInput struct {
	RemoteURL string
	Ref       string
	OutputKey string
}

// ResolveRef resolves a remote ref to a commit SHA. If the ref is already a
// SHA-like revision, git ls-remote may return no rows; that case passes the ref
// through unchanged.
func ResolveRef(ctx context.Context, git GitOps, sink domainci.OutputSink, w io.Writer, in ResolveRefInput) (string, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Ref == "" {
		return "", fmt.Errorf("ref is required: %w", errs.ErrUsage)
	}

	remote := in.RemoteURL
	if remote == "" {
		remote = "https://github.com/diggsweden/reusable-ci"
	}

	key := in.OutputKey
	if key == "" {
		key = "sha"
	}

	if !validOutputKey(key) {
		return "", fmt.Errorf("output-key %q is invalid: %w", key, errs.ErrUsage)
	}

	out, err := git.Run(ctx, "ls-remote", remote, in.Ref)
	if err != nil {
		return "", fmt.Errorf("resolve ref %q in %s: %w", in.Ref, remote, err)
	}

	sha, ok := resolveLSRemoteSHA(in.Ref, out)
	if !ok {
		return "", fmt.Errorf("ref %q was not found in %s: %w", in.Ref, remote, errs.ErrInvalidConfig)
	}

	if err := sink.Set(ctx, key, sha); err != nil {
		return "", err
	}

	if w != nil {
		_, _ = fmt.Fprintln(w, sha)
	}

	return sha, nil
}

func resolveLSRemoteSHA(ref, out string) (string, bool) {
	fallback := ""

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "" {
			continue
		}

		if len(fields) > 1 && strings.HasSuffix(fields[1], "^{}") {
			return fields[0], true
		}

		if fallback == "" {
			fallback = fields[0]
		}
	}

	if fallback != "" {
		return fallback, true
	}

	if isSHARef(ref) {
		return ref, true
	}

	return "", false
}

func isSHARef(ref string) bool {
	if len(ref) != 40 {
		return false
	}

	for _, r := range ref {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}

	return true
}

func validOutputKey(key string) bool {
	if key == "" {
		return false
	}

	for i, r := range key {
		if !validKeyRune(r, i) {
			return false
		}
	}

	return true
}

func validKeyRune(r rune, position int) bool {
	switch {
	case isASCIIAlpha(r), r == '_':
		return true
	case position > 0 && (isASCIIDigit(r) || r == '-'):
		return true
	}

	return false
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

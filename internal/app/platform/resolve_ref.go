// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	domainci "github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
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

// ResolveRef queries exact branch/tag names (not globs). Branch/tag collisions
// and unbound peeled rows refuse independent of output order. A canonical
// 40/64-character object ID passes through only when the remote returns no rows.
func ResolveRef(ctx context.Context, git GitOps, sink domainci.OutputSink, w io.Writer, in ResolveRefInput) (string, error) { //nolint:cyclop,varnamelen // preflight then one exact lookup, strict binding and publication.
	// The writer is optional; the Git port and the output sink are not.
	if git == nil || sink == nil {
		return "", fmt.Errorf("resolve-ref: git and output sink are required: %w", errs.ErrUsage)
	}

	if in.Ref == "" {
		return "", fmt.Errorf("ref is required: %w", errs.ErrUsage)
	}

	remote := in.RemoteURL
	if remote == "" {
		return "", fmt.Errorf("resolve-ref: remote URL is required: %w", errs.ErrUsage)
	}

	if strings.HasPrefix(remote, "-") || !utf8.ValidString(remote) || strings.ContainsFunc(remote, unicode.IsControl) {
		return "", fmt.Errorf("resolve-ref: unsafe remote: %w", errs.ErrUsage)
	}

	if err := validCheckoutRef(in.Ref); err != nil {
		return "", err
	}

	key := in.OutputKey
	if key == "" {
		key = "sha"
	}

	if !validOutputKey(key) {
		return "", fmt.Errorf("output-key %q is invalid: %w", key, errs.ErrUsage)
	}

	patterns := []string{in.Ref}
	if strings.HasPrefix(in.Ref, "refs/tags/") {
		patterns = append(patterns, in.Ref+"^{}")
	} else if in.Ref != "HEAD" && !strings.HasPrefix(in.Ref, "refs/") && !isHexSHA(in.Ref) {
		patterns = []string{"refs/heads/" + in.Ref, "refs/tags/" + in.Ref, "refs/tags/" + in.Ref + "^{}"}
	}

	out, err := git.Run(ctx, append([]string{"ls-remote", "--", remote}, patterns...)...)
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
		if _, err := fmt.Fprintln(w, sha); err != nil {
			return "", fmt.Errorf("write resolved ref: %w", err)
		}
	}

	return sha, nil
}

func resolveLSRemoteSHA(ref, out string) (string, bool) { //nolint:cyclop // one parser enforces row grammar, name/peel binding, consistent format and deterministic ambiguity refusal.
	rows := map[string]string{}

	tag, branch := ref, ref
	if !strings.HasPrefix(ref, "refs/") && ref != "HEAD" {
		tag, branch = "refs/tags/"+ref, "refs/heads/"+ref
	}

	width := 0

	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 2 || !domaingit.ValidCommitSHA(fields[0]) {
			return "", false
		}

		name := fields[1]

		peeledTag := strings.HasPrefix(tag, "refs/tags/") && name == tag+"^{}"
		if name != branch && name != tag && !peeledTag {
			return "", false
		}

		if (width != 0 && width != len(fields[0])) || (rows[name] != "" && rows[name] != fields[0]) {
			return "", false
		}

		width = len(fields[0])
		rows[name] = fields[0]
	}

	if branch != tag && rows[branch] != "" && rows[tag] != "" {
		return "", false
	}

	if peeled := rows[tag+"^{}"]; peeled != "" {
		return peeled, rows[tag] != ""
	}

	if rows[branch] != "" {
		return rows[branch], true
	}

	if rows[tag] != "" {
		return rows[tag], true
	}

	if isHexSHA(ref) && len(rows) == 0 {
		return ref, true
	}

	return "", false
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

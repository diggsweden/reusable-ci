// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package errs holds the small surface main() needs to translate
// errors into process exit codes:
//
//   - ExitCodeType + sysexits.h-aligned constants
//   - ExitCodeFromError(err) — the one mapping main() applies
//   - A couple of sentinels callers branch on with errors.Is
//
// Domain and app code create errors with plain `fmt.Errorf("...: %w",
// err)`. There is no typed-error builder; if future requirements need
// structured exit codes per failure class, add a sentinel + a case in
// ExitCodeFromError. Don't reach for a framework until that demand is
// concrete.
package errs

import (
	"context"
	"errors"
)

// Sentinels callers wrap with `fmt.Errorf("ctx: %w", sentinel)` so
// errors.Is keeps working and ExitCodeFromError can map each shape to
// a sysexits.h exit code. Each sentinel corresponds to one ExitCode*
// value; the mapping lives in ExitCodeFromError below.
//
// Wrap pattern (stdlib-aligned, like fs.ErrPermission):
//
//	return fmt.Errorf("commit-push: BRANCH is required: %w", errs.ErrUsage)
//
// The sentinel appears as the trailing tag in .Error() output; the
// human-actionable context comes first. Operators read it as
// `Error: BRANCH is required: usage error`.
var (
	// ErrUsage marks CLI invocation errors — missing required flags,
	// wrong positional arg count, bad flag values. Maps to ExitCodeUsage (2).
	ErrUsage = errors.New("usage error")

	// ErrInvalidConfig marks malformed config files (artifacts.yml
	// schema violations, parse failures). Maps to ExitCodeConfiguration (78).
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrMissingInput marks references to files / refs / artifacts
	// that don't exist on disk. Maps to ExitCodeNoInput (66).
	ErrMissingInput = errors.New("missing input")

	// ErrMalformedInput marks user-supplied content that exists but
	// fails to parse / decode (JSON, base64, encoded keys). Distinct
	// from ErrInvalidConfig (which is specifically about artifacts.yml
	// schema violations) and from ErrUsage (which is about CLI args).
	// Maps to ExitCodeDataErr (65).
	ErrMalformedInput = errors.New("malformed input")

	// ErrValidation marks domain rule failures — invalid tag format,
	// bad signature, changelog missing, namespace forbidden. The CI
	// step ran fine; the project's state failed a check. Maps to
	// ExitCodeValidation (1).
	ErrValidation = errors.New("validation failed")

	// ErrPermissionDenied marks auth/credential failures — missing
	// token, token rejected by the API, secret not set in env. Maps
	// to ExitCodeNoPerm (77).
	ErrPermissionDenied = errors.New("permission denied")

	// ErrDependencyUnavailable marks external-tool failures — required
	// binary (syft/trivy/gpg/git) missing from PATH or a registry
	// unreachable. Maps to ExitCodeUnavailable (69).
	ErrDependencyUnavailable = errors.New("dependency unavailable")

	// ErrUnsupported marks features the current platform doesn't
	// implement (multi-line outputs on GitLab, SLSA off-GitHub).
	// Provider/adapter implementations return this so callers can
	// branch on errors.Is(err, errs.ErrUnsupported) without depending
	// on the concrete adapter type. Maps to ExitCodeUnavailable (69).
	ErrUnsupported = errors.New("unsupported on this platform")

	// ErrReleaseNotFound is returned by release-provider adapters when
	// a queried release does not exist. Callers that *expect* absence
	// (cleanup-before-publish, "is this a re-run?") match with
	// errors.Is and treat it as a success path; other callers wrap and
	// propagate. Maps to ExitCodeNoInput (66) when it reaches main()
	// unhandled — the user asked for a release that isn't there.
	ErrReleaseNotFound = errors.New("release not found")
)

// ExitCodeType classifies an error into the process exit code main()
// should return. Aligned with BSD sysexits.h for non-1/non-2 codes.
type ExitCodeType int

// Exit codes — POSIX 1/2 keep their conventional meanings; values >=64
// come from BSD sysexits.h.
const (
	ExitCodeOK            ExitCodeType = 0  // success
	ExitCodeValidation    ExitCodeType = 1  // rule failure (POSIX)
	ExitCodeUsage         ExitCodeType = 2  // CLI misuse / context-cancel
	ExitCodeDataErr       ExitCodeType = 65 // EX_DATAERR: malformed input
	ExitCodeNoInput       ExitCodeType = 66 // EX_NOINPUT: missing input file/ref
	ExitCodeUnavailable   ExitCodeType = 69 // EX_UNAVAILABLE: external dep missing
	ExitCodeSoftware      ExitCodeType = 70 // EX_SOFTWARE: internal bug
	ExitCodeNoPerm        ExitCodeType = 77 // EX_NOPERM: auth / permission
	ExitCodeConfiguration ExitCodeType = 78 // EX_CONFIG: bad config file
)

// FromHTTPStatus classifies an HTTP response status into one of the
// auth/dependency sentinels. Returns nil for status codes that don't
// have a clear mapping — callers either inline the catch-all
// (untyped → Software) or layer on a domain-specific sentinel.
//
// Used by adapter/github + adapter/gitlab + app/security/upload_sarif
// where the non-2xx path is otherwise the same wrap shape.
func FromHTTPStatus(status int) error {
	switch {
	case status == 401, status == 403:
		return ErrPermissionDenied
	case status == 404:
		return ErrMissingInput
	case status >= 500:
		return ErrDependencyUnavailable
	}
	return nil
}

// ExitCodeFromError maps an error to the process exit code main()
// should return. Each sentinel above corresponds to one case here;
// unrecognised non-nil errors fall through to ExitCodeSoftware — the
// "we got an error we didn't classify" bucket. The order of cases
// matters only insofar as a single err.Is might match multiple
// sentinels in pathological wraps; in practice each error matches
// one classification.
func ExitCodeFromError(err error) ExitCodeType {
	switch {
	case err == nil:
		return ExitCodeOK
	case errors.Is(err, context.Canceled):
		return ExitCodeUsage
	case errors.Is(err, ErrUsage):
		return ExitCodeUsage
	case errors.Is(err, ErrValidation):
		return ExitCodeValidation
	case errors.Is(err, ErrMalformedInput):
		return ExitCodeDataErr
	case errors.Is(err, ErrMissingInput), errors.Is(err, ErrReleaseNotFound):
		return ExitCodeNoInput
	case errors.Is(err, ErrUnsupported), errors.Is(err, ErrDependencyUnavailable):
		return ExitCodeUnavailable
	case errors.Is(err, ErrPermissionDenied):
		return ExitCodeNoPerm
	case errors.Is(err, ErrInvalidConfig):
		return ExitCodeConfiguration
	default:
		return ExitCodeSoftware
	}
}

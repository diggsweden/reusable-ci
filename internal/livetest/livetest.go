// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package livetest is the kit for the live-forge conformance tier: the tests
// that drive this repository's real provider adapters against real Forgejo and
// GitLab servers instead of an httptest fake.
//
// # Why this tier exists
//
// Every adapter is currently verified against a fake we wrote from the same
// assumptions as the adapter. An assumption that is wrong is therefore wrong in
// both places, and both agree. This tier is where a real server gets a vote.
//
// It is also the only place the product claim can be tested: that a verb means
// the same thing on every forge. One scenario runs against every provider that
// claims the capability, and the same outcome is asserted for each.
//
// # Not the black-box suite
//
// Keep these apart. The black-box suite lives in its own repository
// (reusable-ci-blackbox-tests), drives the compiled binary against real
// toolchains with the network faked, and deliberately excludes forge APIs. This
// tier is the opposite trade: real forge APIs, no toolchain claims. A scenario
// that needs syft, gpg, or a reproducible archive belongs there, not here.
//
// # Not lab-specific
//
// The environment contract this reads is git-provider-lab's, because that is
// what exists and what is safe to destroy. Nothing here assumes it: a Target is
// a host, an owner, a token, and a namespace. When a GitHub tier arrives it
// supplies those four the same way.
//
// # Safety
//
// Every scenario mutates a real server, so nothing runs without four
// independent agreements: the `live` build tag, a sourced schema-2 contract, a
// destructive confirmation the operator types separately, and a run identity
// that matches the contract exactly — including the resource namespace this
// suite declared it owns. A normal `go test ./...` never sees any of it.
package livetest

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TB is the slice of *testing.T this kit needs, kept as an interface so the
// package stays free of the testing import and can be reasoned about (and
// unit-tested) as ordinary code.
type TB interface {
	Helper()
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Cleanup(f func())
}

// ResourcePrefix is the namespace this suite owns on every forge it touches.
// It is declared to git-provider-lab when the contract is minted
// (`emit-targets.sh --resource-prefix rc-`) and re-checked here, so a contract
// minted for another consumer cannot authorize this suite to delete that
// consumer's fixtures.
const ResourcePrefix = "rc-"

const (
	contractSchemaVersion = "2"

	// confirmDestroyEnv is deliberately outside the REUSABLE_CI_* namespace:
	// that namespace belongs to the product's own flags, and a variable that
	// only arms a test suite must not look like one of them.
	confirmDestroyEnv = "RC_LIVE_CONFIRM_DESTROY"
	confirmDestroy    = "destroy-live-forge-fixtures"

	maxUnexpiringTokenAge    = 24 * time.Hour
	allowedClockSkew         = 5 * time.Minute
	maximumNativeTokenExpiry = 31 * 24 * time.Hour
)

var (
	runIDPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,39}$`)
	resourcePrefixRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}-$`)
	ownerPattern         = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	hostnamePattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	rfc3339Pattern       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
)

// Target names one selected forge instance and the identity to act as.
type Target struct {
	Kind  provider.Platform
	Host  string // host[:port], no scheme
	Owner string
	Token string

	// accepted is set only by the destructive guard. Every mutating helper
	// refuses a Target without it, so a hand-built Target cannot reach a
	// server.
	accepted bool
}

// BaseURL is the https origin the adapters and the raw oracle both address.
func (t Target) BaseURL() string { return "https://" + t.Host }

// String names a target for test output without exposing its token.
func (t Target) String() string { return string(t.Kind) + "@" + t.Host + "/" + t.Owner }

// targetRef is one selected forge's identity-bearing fields. Keeping these on
// the contract rather than re-reading the environment is what lets the identity
// derivation stay pure, and therefore testable without a lab or a mutated
// process environment.
type targetRef struct {
	kind  string
	host  string
	owner string
}

// contract is the sourced environment, read once and validated as a whole.
type contract struct {
	schemaVersion  string
	runID          string
	refs           []targetRef
	resourcePrefix string
	identity       string
	confirmation   string
	cleanupCommand string
}

type tokenMetadata struct {
	id                 string
	name               string
	createdAt          string
	expiresAt          string
	revocationRequired string
	revocationRunID    string
}

// Selected reports whether the sourced contract selected a forge. Scenarios use
// it to skip cleanly rather than fail when an operator minted a contract for a
// subset of providers.
func Selected(kind provider.Platform) bool {
	for _, name := range strings.Split(os.Getenv("LAB_TARGETS"), ",") {
		if name == string(kind) {
			return true
		}
	}

	return false
}

// Accept reads the contract, validates every safety rule, and returns a Target
// armed for mutation. It fails the test rather than returning an error: a
// half-armed target is not a thing a scenario should be able to hold.
func Accept(tb TB, kind provider.Platform) Target {
	tb.Helper()

	if !Selected(kind) {
		tb.Skipf("livetest: %s not selected by LAB_TARGETS", kind)
	}

	prefix := "LAB_" + strings.ToUpper(string(kind))
	target := Target{
		Kind:  kind,
		Host:  os.Getenv(prefix + "_HOST"),
		Owner: os.Getenv(prefix + "_OWNER"),
		Token: os.Getenv(prefix + "_TOKEN"),
	}

	sourced := contract{
		schemaVersion:  os.Getenv("LAB_TARGETS_SCHEMA_VERSION"),
		runID:          os.Getenv("LAB_RUN_ID"),
		refs:           selectedRefs(),
		resourcePrefix: os.Getenv("LAB_RESOURCE_PREFIX"),
		identity:       os.Getenv("LAB_LIVE_EXPECTED_IDENTITY"),
		confirmation:   os.Getenv(confirmDestroyEnv),
		cleanupCommand: os.Getenv("LAB_TOKEN_CLEANUP_CMD"),
	}

	token := tokenMetadata{
		id:                 os.Getenv(prefix + "_TOKEN_ID"),
		name:               os.Getenv(prefix + "_TOKEN_NAME"),
		createdAt:          os.Getenv(prefix + "_TOKEN_CREATED_AT"),
		expiresAt:          os.Getenv(prefix + "_TOKEN_EXPIRES_AT"),
		revocationRequired: os.Getenv(prefix + "_TOKEN_REVOCATION_REQUIRED"),
		revocationRunID:    os.Getenv(prefix + "_TOKEN_REVOCATION_RUN_ID"),
	}

	if err := validate(target, sourced, token, time.Now()); err != nil {
		tb.Fatalf("livetest: destructive guard refused this run: %v", err)
	}

	target.accepted = true

	return target
}

// validate is the whole guard, pure and table-testable. Order matters only for
// the quality of the message: every rule is mandatory.
func validate(target Target, sourced contract, token tokenMetadata, now time.Time) error {
	if err := validateContractShape(sourced); err != nil {
		return err
	}

	if err := validateTarget(target); err != nil {
		return err
	}

	if err := validateTokenMetadata(target.Kind, sourced.runID, token, now); err != nil {
		return err
	}

	return validateAuthorization(target, sourced)
}

// validateContractShape checks the run-wide fields: the ones that are wrong for
// every target at once if they are wrong at all.
func validateContractShape(sourced contract) error {
	if sourced.schemaVersion != contractSchemaVersion {
		return fmt.Errorf("contract schema must be %s, got %q: %w", contractSchemaVersion, sourced.schemaVersion, errs.ErrValidation)
	}

	if !runIDPattern.MatchString(sourced.runID) {
		return fmt.Errorf("run ID %q does not match [a-z0-9][a-z0-9-]{2,39}: %w", sourced.runID, errs.ErrValidation)
	}

	// The prefix must both be well-formed and be *ours*. A contract minted for
	// another consumer is well-formed and still must not arm this suite.
	if !resourcePrefixRegexp.MatchString(sourced.resourcePrefix) {
		return fmt.Errorf("resource prefix %q is not a namespace: %w", sourced.resourcePrefix, errs.ErrValidation)
	}

	if sourced.resourcePrefix != ResourcePrefix {
		return fmt.Errorf("contract declares namespace %q, but this suite owns %q: %w", sourced.resourcePrefix, ResourcePrefix, errs.ErrValidation)
	}

	if !strings.HasPrefix(sourced.cleanupCommand, "/") {
		return fmt.Errorf("token cleanup command must be an absolute path, got %q: %w", sourced.cleanupCommand, errs.ErrValidation)
	}

	return nil
}

// validateAuthorization is the pair that turns a well-formed contract into
// permission to destroy: an identity this suite re-derived, and a confirmation
// the operator typed against that exact identity.
func validateAuthorization(target Target, sourced contract) error {
	wantIdentity, err := Identity(sourced.runID, sourced.refs, sourced.resourcePrefix)
	if err != nil {
		return err
	}

	if sourced.identity != wantIdentity {
		return fmt.Errorf("contract identity does not match this run, hosts, owners, and namespace: %w", errs.ErrValidation)
	}

	// The target being acted on must appear in the identity by exact string,
	// not merely be one of the selected names.
	want := identityEntry(string(target.Kind), target.Host, target.Owner, sourced.resourcePrefix)
	if !strings.Contains(sourced.identity, want) {
		return fmt.Errorf("identity does not contain the exact target %q: %w", want, errs.ErrValidation)
	}

	if sourced.confirmation != confirmDestroy+"|"+wantIdentity {
		return fmt.Errorf("%s must equal %q: %w", confirmDestroyEnv, confirmDestroy+"|"+wantIdentity, errs.ErrValidation)
	}

	return nil
}

func validateTarget(target Target) error {
	switch target.Kind {
	case provider.PlatformGitLab, provider.PlatformForgejo:
	case provider.PlatformGitHub, provider.PlatformLocal:
		return fmt.Errorf("platform %q is not a live-forge target in this tier: %w", target.Kind, errs.ErrValidation)
	default:
		return fmt.Errorf("unknown platform %q: %w", target.Kind, errs.ErrValidation)
	}

	if target.Token == "" {
		return fmt.Errorf("%s token is empty: %w", target.Kind, errs.ErrValidation)
	}

	if !ownerPattern.MatchString(target.Owner) || target.Owner == "." || target.Owner == ".." {
		return fmt.Errorf("owner %q is not a resource owner: %w", target.Owner, errs.ErrValidation)
	}

	return validateDisposableHost(target.Host)
}

// validateDisposableHost accepts only hosts this tier is allowed to destroy.
// The suffix is compiled in on purpose: an allowlist read from the environment
// would be one typo away from pointing a destructive suite at production, and
// the environment here is exactly what an attacker or an accident controls.
func validateDisposableHost(host string) error {
	hostname := host

	if strings.Contains(host, ":") {
		split, port, err := net.SplitHostPort(host)
		if err != nil {
			return fmt.Errorf("host %q is not host[:port]: %w", host, errs.ErrValidation)
		}

		number, convErr := strconv.Atoi(port)
		if convErr != nil || number < 1 || number > 65535 {
			return fmt.Errorf("host %q has an invalid port: %w", host, errs.ErrValidation)
		}

		hostname = split
	}

	if len(hostname) > 253 || !hostnamePattern.MatchString(hostname) {
		return fmt.Errorf("host %q is not a hostname: %w", host, errs.ErrValidation)
	}

	if !strings.HasSuffix(hostname, ".gitproviderlab") {
		return fmt.Errorf("host %q is outside the disposable-forge suffix this tier may mutate: %w", host, errs.ErrValidation)
	}

	return nil
}

// validateTokenMetadata refuses a credential that outlives the run. GitLab can
// attach a native expiry; Forgejo's API cannot, so it must instead be freshly
// minted and carry run-bound revocation metadata.
func validateTokenMetadata(kind provider.Platform, runID string, token tokenMetadata, now time.Time) error {
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(token.id) {
		return fmt.Errorf("%s token ID must be numeric: %w", kind, errs.ErrValidation)
	}

	if token.name != "lab-targets-"+runID {
		return fmt.Errorf("%s token name is not bound to run %q: %w", kind, runID, errs.ErrValidation)
	}

	if !rfc3339Pattern.MatchString(token.createdAt) {
		return fmt.Errorf("%s token creation time must be RFC3339: %w", kind, errs.ErrValidation)
	}

	createdAt, err := time.Parse(time.RFC3339, token.createdAt)
	if err != nil {
		return fmt.Errorf("%s token creation time: %w: %w", kind, err, errs.ErrValidation)
	}

	if createdAt.After(now.Add(allowedClockSkew)) {
		return fmt.Errorf("%s token creation time is in the future: %w", kind, errs.ErrValidation)
	}

	if token.expiresAt == "" {
		return validateRevocableToken(kind, runID, token, createdAt, now)
	}

	return validateExpiringToken(kind, token, now)
}

// validateRevocableToken covers the forges whose API cannot attach an expiry:
// the credential must instead be freshly minted and revocable with this run.
func validateRevocableToken(kind provider.Platform, runID string, token tokenMetadata, createdAt, now time.Time) error {
	{
		if createdAt.Before(now.Add(-maxUnexpiringTokenAge)) {
			return fmt.Errorf("%s non-expiring token is older than 24 hours: %w", kind, errs.ErrValidation)
		}

		if token.revocationRequired != "true" || token.revocationRunID != runID {
			return fmt.Errorf("%s non-expiring token requires run-bound revocation metadata: %w", kind, errs.ErrValidation)
		}

		return nil
	}
}

// validateExpiringToken covers a natively expiring credential: the expiry must
// be real, soon, and not paired with revocation metadata that contradicts it.
func validateExpiringToken(kind provider.Platform, token tokenMetadata, now time.Time) error {
	if kind != provider.PlatformGitLab {
		return fmt.Errorf("%s has no native token expiry; revocation metadata is required: %w", kind, errs.ErrValidation)
	}

	expiry, err := parseExpiry(token.expiresAt)
	if err != nil {
		return fmt.Errorf("%s token expiry: %w: %w", kind, err, errs.ErrValidation)
	}

	if !expiry.After(now.Add(allowedClockSkew)) || expiry.After(now.Add(maximumNativeTokenExpiry)) {
		return fmt.Errorf("%s token expiry must be between 5 minutes and 31 days from now: %w", kind, errs.ErrValidation)
	}

	if token.revocationRequired != "false" || token.revocationRunID != "" {
		return fmt.Errorf("%s native expiry conflicts with revocation fallback metadata: %w", kind, errs.ErrValidation)
	}

	return nil
}

func parseExpiry(value string) (time.Time, error) {
	if expiry, err := time.Parse(time.RFC3339, value); err == nil {
		return expiry, nil
	}

	if expiry, err := time.Parse(time.DateOnly, value); err == nil {
		return expiry, nil
	}

	return time.Time{}, fmt.Errorf("%q is neither RFC3339 nor a date: %w", value, errs.ErrMalformedInput)
}

// selectedRefs reads the identity-bearing fields of every selected target. This
// is the one place the environment is consulted; everything downstream is pure.
func selectedRefs() []targetRef {
	names := strings.Split(os.Getenv("LAB_TARGETS"), ",")
	refs := make([]targetRef, 0, len(names))

	for _, kind := range names {
		envPrefix := "LAB_" + strings.ToUpper(kind)
		refs = append(refs, targetRef{
			kind:  kind,
			host:  os.Getenv(envPrefix + "_HOST"),
			owner: os.Getenv(envPrefix + "_OWNER"),
		})
	}

	return refs
}

// Identity re-derives the run identity from the contract's parts. The guard
// compares this against the value the contract carries: re-deriving is the
// point, since a guard that reads the assertion it is checking asserts nothing.
//
// Every selected provider contributes an entry, including gitea, which no
// adapter here targets: its token is still in the run's cleanup scope, so
// leaving it out of the identity would let a contract be silently reused with a
// provider the operator never approved.
func Identity(runID string, refs []targetRef, resourcePrefix string) (string, error) {
	if !runIDPattern.MatchString(runID) {
		return "", fmt.Errorf("run ID %q is invalid: %w", runID, errs.ErrValidation)
	}

	entries := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))

	for _, ref := range refs {
		if ref.kind == "" || seen[ref.kind] {
			return "", fmt.Errorf("selected providers are empty or repeated: %w", errs.ErrValidation)
		}

		seen[ref.kind] = true

		switch ref.kind {
		case string(provider.PlatformGitLab), string(provider.PlatformForgejo), "gitea":
		default:
			return "", fmt.Errorf("unknown provider %q is selected: %w", ref.kind, errs.ErrValidation)
		}

		if err := validateDisposableHost(ref.host); err != nil {
			return "", err
		}

		if !ownerPattern.MatchString(ref.owner) {
			return "", fmt.Errorf("%s owner %q is not a resource owner: %w", ref.kind, ref.owner, errs.ErrValidation)
		}

		entries = append(entries, identityEntry(ref.kind, ref.host, ref.owner, resourcePrefix))
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("no provider is selected: %w", errs.ErrValidation)
	}

	return "run=" + runID + "|targets=" + strings.Join(entries, ","), nil
}

func identityEntry(kind, host, owner, resourcePrefix string) string {
	return kind + "@https://" + host + "/" + owner + "#resources=" + resourcePrefix
}

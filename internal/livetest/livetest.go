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
// This reads the neutral live-target contract: one JSON object describing
// disposable provider infrastructure. Forge Lab produces it, but nothing here
// assumes Forge Lab — any operator willing to meet the documented shape can
// supply one. A Target carries the selected forge identity plus its declared
// registry, transport CA, and Fulcio issuer facts.
//
// # Safety
//
// Every scenario mutates a real server, so nothing runs without four
// independent agreements: the `live` build tag, a contract naming hosts this
// tier is compiled to accept as disposable, an owner the operator declared per
// forge, and a destructive confirmation typed against the run identity this
// suite derives from all three. A normal `go test ./...` never sees any of it.
//
// The contract supplies infrastructure facts only. It carries no namespace, no
// owner, and no confirmation, because a producer that supplied those would be
// handing out permission along with the address — and any consumer holding the
// file could then act on any other consumer's fixtures.
package livetest

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
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
//
// It is this suite's own declaration, not something a producer tells it. The
// neutral contract carries no namespace on purpose -- a producer that named one
// would be authorizing a consumer it knows nothing about -- so the prefix is
// compiled in here and every resource this tier creates or destroys is required
// to sit under it.
const ResourcePrefix = "rc-"

const (
	// contractFileEnv names the neutral live-target contract. The producer is
	// Forge Lab, or any operator willing to meet the same documented shape.
	contractFileEnv        = "LAB_TARGETS_FILE"
	frozenContractFactsEnv = "RC_LIVE_CONTRACT_FACTS"
	frozenCAFactsEnv       = "RC_LIVE_CA_FACTS"
	proxyBinaryEnv         = "RC_LIVE_CREDENTIAL_PROXY_BIN"

	// ownerEnvPrefix is how the operator declares which owner on each forge
	// this run may act under. It is a consumer concern, so it lives in this
	// suite's namespace rather than the producer's: RC_LIVE_GITLAB_OWNER.
	ownerEnvPrefix = "RC_LIVE_"

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
	runIDPattern         = regexp.MustCompile(`^[a-z][a-z0-9-]{2,39}$`)
	resourcePrefixRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}-$`)
	ownerPattern         = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	hostnamePattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	rfc3339Pattern       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

	// disposableHostPattern is the set of hosts this tier may destroy things
	// on. It is compiled in, and it names the service and the road as well as
	// the domain, because an allowlist read from the environment would be one
	// typo away from pointing a destructive suite at production and the
	// environment is exactly what an accident controls.
	//
	// This is the pattern Forge Lab publishes for its own instances. A bare
	// domain suffix would also have been correct and weaker: it would admit any
	// host someone could get that suffix onto, including one that is not a
	// forge at all.
	disposableHostPattern = regexp.MustCompile(`^(gitlab|gitea|forgejo)\.(compose|k3s)\.forgelab$`)
)

// Target names one selected forge instance and the identity to act as.
type Target struct {
	Forge               provider.ForgeAPI
	Host                string // host[:port], no scheme
	Owner               string
	Token               string
	CredentialUsername  string
	RegistryOrigin      string
	ForgeAuthorities    []string
	RegistryAuthorities []string
	OIDCAuthorities     []string
	Capabilities        []string
	CAFile              string
	CAFacts             string
	FulcioURL           string
	OIDCIssuer          string
	caPEM               []byte

	// accepted is set only by the destructive guard. Every mutating helper
	// refuses a Target without it, so a hand-built Target cannot reach a
	// server.
	accepted bool
}

// BaseURL is the https origin the adapters and the raw oracle both address.
func (t Target) BaseURL() string { return "https://" + t.Host }

// String names a target for test output without exposing its token.
func (t Target) String() string { return string(t.Forge) + "@" + t.Host + "/" + t.Owner }

// targetRef is one selected forge's identity-bearing fields. Keeping these on
// the contract rather than re-reading the environment is what lets the identity
// derivation stay pure, and therefore testable without a lab or a mutated
// process environment.
type targetRef struct {
	forge string
	host  string
	owner string
}

// contract is the run's authorization, assembled once and validated as a whole.
//
// It is no longer one sourced environment. The producer supplies the run
// identifier, the endpoints, and the credentials; this suite supplies the
// namespace it owns and the owners it was told to act under; and the operator
// supplies the confirmation. Keeping them in one struct keeps the guard a pure
// function of its inputs, which is what makes it table-testable without a lab.
type contract struct {
	runID               string
	refs                []targetRef
	resourcePrefix      string
	confirmation        string
	cleanupCommand      string
	cleanupContractFile string
}

type tokenMetadata struct {
	id        string
	name      string
	createdAt string
	expiresAt string

	// revocationRequired is a bool rather than the "true"/"false" string this
	// once read out of the environment. The contract carries a JSON boolean, so
	// a string here would be a stringly-typed round trip whose only remaining
	// job was to be compared against a literal.
	revocationRequired bool
	revocationRunID    string
}

// Selected reports whether the contract carries a forge and the operator
// declared an owner for it. Scenarios use it to skip cleanly rather than fail
// when a contract was minted for a subset of providers.
//
// Both halves are required. A contract endpoint without a declared owner is
// infrastructure this run was shown but not authorized to touch, and treating
// it as selected would make the skip depend on the producer alone.
func Selected(tb TB, forge provider.ForgeAPI) bool {
	tb.Helper()

	contract, err := currentLabContract()
	if err != nil {
		tb.Fatalf("livetest: %s: %v", contractFileEnv, err)

		return false
	}

	if ownerFor(forge) == "" {
		return false
	}

	_, err = selectedEndpointFor(contract, forge)
	if err != nil && !strings.Contains(err.Error(), "selects no") {
		tb.Fatalf("livetest: %v", err)
	}

	return err == nil
}

// ownerFor reads the owner the operator declared for one forge.
func ownerFor(forge provider.ForgeAPI) string {
	return os.Getenv(ownerEnvPrefix + strings.ToUpper(string(forge)) + "_OWNER")
}

func endpointNameFor(forge provider.ForgeAPI) string {
	return os.Getenv(ownerEnvPrefix + strings.ToUpper(string(forge)) + "_ENDPOINT")
}

func selectedEndpointFor(contract labContract, forge provider.ForgeAPI) (labEndpoint, error) {
	return contract.endpointFor(string(forge), endpointNameFor(forge))
}

// Accept reads the contract, validates every safety rule, and returns a Target
// armed for mutation. It fails the test rather than returning an error: a
// half-armed target is not a thing a scenario should be able to hold.
func Accept(tb TB, forge provider.ForgeAPI) Target { //nolint:cyclop,govet // Ordered guard projection deliberately fails through TB.
	tb.Helper()

	if !Selected(tb, forge) {
		tb.Skipf("livetest: %s has no endpoint and explicit %s%s_OWNER", forge, ownerEnvPrefix, strings.ToUpper(string(forge)))
	}

	lab, err := currentLabContract()
	if err != nil {
		tb.Fatalf("livetest: %s: %v", contractFileEnv, err)
	}

	endpoint, err := selectedEndpointFor(lab, forge)
	if err != nil {
		tb.Fatalf("livetest: %v", err)
	}

	if capabilityErr := endpoint.requireLiveCapabilities(); capabilityErr != nil {
		tb.Fatalf("livetest: %v", capabilityErr)
	}

	target, err := targetFromContract(lab, endpoint, forge, ownerFor(forge))
	if err != nil {
		tb.Fatalf("livetest: endpoint trust boundary refused this run: %v", err)
	}

	target, err = captureTargetCA(target)
	if err != nil {
		tb.Fatalf("livetest: frozen ca_file refused this run: %v", err)
	}

	cleanupCommand, cleanupContractFile := lab.cleanupPair()

	refs, err := selectedRefs(lab)
	if err != nil {
		tb.Fatalf("livetest: selected endpoint projection failed: %v", err)
	}

	sourced := contract{
		runID:               lab.Generation.ID,
		refs:                refs,
		resourcePrefix:      ResourcePrefix,
		confirmation:        os.Getenv(confirmDestroyEnv),
		cleanupCommand:      cleanupCommand,
		cleanupContractFile: cleanupContractFile,
	}

	if err := validate(target, sourced, endpoint.tokenMeta(lab.Generation.ID), time.Now()); err != nil {
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

	if err := validateTokenMetadata(target.Forge, sourced.runID, token, now); err != nil {
		return err
	}

	return validateAuthorization(target, sourced)
}

// validateContractShape checks the run-wide fields: the ones that are wrong for
// every target at once if they are wrong at all.
func validateContractShape(sourced contract) error {
	if !runIDPattern.MatchString(sourced.runID) {
		return fmt.Errorf("generation ID %q does not match [a-z][a-z0-9-]{2,39}: %w", sourced.runID, errs.ErrValidation)
	}

	// The namespace is this suite's own constant rather than a producer's
	// claim, so the check is that the constant is still well-formed -- a
	// malformed one would widen what the identity below binds.
	if !resourcePrefixRegexp.MatchString(sourced.resourcePrefix) {
		return fmt.Errorf("resource prefix %q is not a namespace: %w", sourced.resourcePrefix, errs.ErrValidation)
	}

	if sourced.resourcePrefix != ResourcePrefix {
		return fmt.Errorf("run declares namespace %q, but this suite owns %q: %w", sourced.resourcePrefix, ResourcePrefix, errs.ErrValidation)
	}

	// Cleanup is not optional for this tier. A producer exposing no revocation
	// interface cannot promise the run's credentials die with the run, and the
	// recipe's exit trap has nothing to call.
	if !strings.HasPrefix(sourced.cleanupCommand, "/") {
		return fmt.Errorf("credential cleanup command must be an absolute path, got %q: %w", sourced.cleanupCommand, errs.ErrValidation)
	}

	if !strings.HasPrefix(sourced.cleanupContractFile, "/") {
		return fmt.Errorf("credential cleanup contract_file must be an absolute path, got %q: %w", sourced.cleanupContractFile, errs.ErrValidation)
	}

	if len(sourced.refs) == 0 {
		return fmt.Errorf("no endpoint in the contract has a declared %s<FORGE>_OWNER: %w", ownerEnvPrefix, errs.ErrValidation)
	}

	return nil
}

// validateAuthorization is what turns a well-formed contract into permission to
// destroy: an identity this suite derives from the run's own parts, and a
// confirmation the operator typed against that exact identity.
//
// The producer no longer echoes an identity back. Under the neutral contract it
// never knew one, and a value it echoed would not have been independent
// evidence anyway -- it would have been this suite's own derivation returned to
// it. What survives is the half that was doing the work: the operator must have
// typed a string naming this run, these hosts, these owners, and this
// namespace, so a contract minted for a different set cannot arm the suite
// without a human retyping the difference.
func validateAuthorization(target Target, sourced contract) error {
	wantIdentity, err := Identity(sourced.runID, sourced.refs, sourced.resourcePrefix)
	if err != nil {
		return err
	}

	// The target being acted on must appear in the identity by exact string,
	// not merely be one of the endpoints the contract carried.
	want := identityEntry(string(target.Forge), target.Host, target.Owner, sourced.resourcePrefix)
	if !strings.Contains(wantIdentity, want) {
		return fmt.Errorf("identity does not contain the exact target %q: %w", want, errs.ErrValidation)
	}

	if sourced.confirmation != confirmDestroy+"|"+wantIdentity {
		return fmt.Errorf("%s must equal %q: %w", confirmDestroyEnv, confirmDestroy+"|"+wantIdentity, errs.ErrValidation)
	}

	return nil
}

func validateTarget(target Target) error {
	switch target.Forge {
	case provider.ForgeGitLab, provider.ForgeForgejo:
	case provider.ForgeGitHub, provider.ForgeLocal:
		return fmt.Errorf("platform %q is not a live-forge target in this tier: %w", target.Forge, errs.ErrValidation)
	default:
		return fmt.Errorf("unknown platform %q: %w", target.Forge, errs.ErrValidation)
	}

	if target.Token == "" {
		return fmt.Errorf("%s token is empty: %w", target.Forge, errs.ErrValidation)
	}

	if target.CredentialUsername == "" {
		return fmt.Errorf("%s credential username is empty: %w", target.Forge, errs.ErrValidation)
	}

	if !ownerPattern.MatchString(target.Owner) || target.Owner == "." || target.Owner == ".." {
		return fmt.Errorf("owner %q is not a resource owner: %w", target.Owner, errs.ErrValidation)
	}

	if err := validateDisposableHost(target.Host); err != nil {
		return err
	}

	return validateTargetAuthorities(target)
}

func validateSelectedEndpoint(endpoint labEndpoint, target Target) error {
	web, err := parseHTTPSURL(endpoint.Kind+" web_base_url", endpoint.WebBaseURL, true)
	if err != nil {
		return err
	}

	api, err := parseHTTPSURL(endpoint.Kind+" api_base_url", endpoint.APIBaseURL, false)
	if err != nil {
		return err
	}

	git, err := parseHTTPSURL(endpoint.Kind+" git_base_url", endpoint.gitURL(), true)
	if err != nil {
		return err
	}

	if web.host != api.host || web.host != git.host {
		return fmt.Errorf("%s web, API, and Git URLs must share one local authority: %w", endpoint.Kind, errs.ErrValidation)
	}

	wantAPIPath := "/api/v1"
	if endpoint.Kind == string(provider.ForgeGitLab) {
		wantAPIPath = "/api/v4"
	}

	if api.path != wantAPIPath {
		return fmt.Errorf("%s api_base_url path must equal %q: %w", endpoint.Kind, wantAPIPath, errs.ErrValidation)
	}

	if target.Host != api.host {
		return fmt.Errorf("%s projected host does not match api_base_url: %w", endpoint.Kind, errs.ErrValidation)
	}

	return validateTargetAuthorities(target)
}

func validateLocalContractTopology(contract labContract) error {
	for _, endpoint := range contract.Endpoints {
		web, err := parseHTTPSURL(endpoint.Kind+" web_base_url", endpoint.WebBaseURL, true)
		if err != nil {
			return err
		}

		if !strings.HasSuffix(web.hostname, ".forgelab") {
			continue
		}

		host, err := endpoint.host()
		if err != nil {
			return err
		}

		fulcioURL, oidcIssuer, _ := contract.fulcioFor(endpoint.Name)

		target := Target{
			Forge:          provider.ForgeAPI(endpoint.Kind),
			Host:           host,
			RegistryOrigin: endpoint.registryOrigin(),
			FulcioURL:      fulcioURL,
			OIDCIssuer:     oidcIssuer,
		}
		if err := validateSelectedEndpoint(endpoint, target); err != nil {
			return err
		}
	}

	return nil
}

func validateTargetAuthorities(target Target) error { //nolint:cyclop // Explicitly validates each approved local authority relationship.
	if target.RegistryOrigin != "" {
		registry, err := parseHTTPSURL(string(target.Forge)+" OCI registry", target.RegistryOrigin, true)
		if err != nil {
			return err
		}

		forgeHost, forgePort, err := splitValidatedHost(target.Host)
		if err != nil {
			return err
		}

		wantRegistryHost := forgeHost
		if target.Forge == provider.ForgeGitLab {
			wantRegistryHost = "registry." + forgeHost
		}

		if registry.hostname != wantRegistryHost || effectivePort(registry.port) != effectivePort(forgePort) {
			return fmt.Errorf("%s OCI registry %q is not the accepted local relationship to %q: %w",
				target.Forge, registry.host, target.Host, errs.ErrValidation)
		}
	}

	if target.FulcioURL == "" && target.OIDCIssuer == "" {
		return nil
	}

	if target.FulcioURL == "" || target.OIDCIssuer == "" {
		return fmt.Errorf("%s Fulcio URL and issuer mapping must be supplied together: %w", target.Forge, errs.ErrValidation)
	}

	fulcio, err := parseHTTPSURL("fulcio.base_url", target.FulcioURL, true)
	if err != nil {
		return err
	}

	forgeHost, forgePort, err := splitValidatedHost(target.Host)
	if err != nil {
		return err
	}

	labels := strings.Split(forgeHost, ".")
	if len(labels) != 3 {
		return fmt.Errorf("forge host %q has no Forge Lab road: %w", forgeHost, errs.ErrValidation)
	}

	wantFulcioHost := "fulcio." + labels[1] + ".forgelab"
	if fulcio.hostname != wantFulcioHost || effectivePort(fulcio.port) != effectivePort(forgePort) {
		return fmt.Errorf("fulcio %q is not on the selected forge road and edge port: %w", fulcio.host, errs.ErrValidation)
	}

	wantIssuer := target.BaseURL()
	if target.Forge == provider.ForgeForgejo {
		wantIssuer += "/api/actions"
	}

	if target.OIDCIssuer != wantIssuer {
		return fmt.Errorf("%s OIDC issuer must equal the contract endpoint mapping %q, got %q: %w",
			target.Forge, wantIssuer, target.OIDCIssuer, errs.ErrValidation)
	}

	return nil
}

func splitValidatedHost(host string) (string, string, error) {
	if err := validateDisposableHost(host); err != nil {
		return "", "", err
	}

	if !strings.Contains(host, ":") {
		return host, "", nil
	}

	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		return "", "", fmt.Errorf("host %q is not host[:port]: %w", host, errs.ErrValidation)
	}

	return hostname, port, nil
}

func effectivePort(port string) string {
	if port == "" {
		return "443"
	}

	return port
}

// FulcioConfig returns the exact contract mapping only after rechecking the
// accepted target's local Fulcio and issuer relationship at the use boundary.
func FulcioConfig(tb TB, target Target) (string, string) {
	tb.Helper()
	requireAccepted(tb, target)

	if target.FulcioURL == "" || target.OIDCIssuer == "" {
		tb.Fatalf("livetest: target has no Fulcio issuer mapping")
	}

	if err := validateTargetAuthorities(target); err != nil {
		tb.Fatalf("livetest: refusing to send an OIDC token to Fulcio: %v", err)
	}

	return target.FulcioURL, target.OIDCIssuer
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

	if !disposableHostPattern.MatchString(hostname) {
		return fmt.Errorf("host %q is not a disposable lab forge this tier may mutate: %w", host, errs.ErrValidation)
	}

	return nil
}

// validateTokenMetadata refuses a credential that outlives the run. GitLab can
// attach a native expiry; Forgejo's API cannot, so it must instead be freshly
// minted and carry run-bound revocation metadata.
func validateTokenMetadata(forge provider.ForgeAPI, runID string, token tokenMetadata, now time.Time) error {
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(token.id) {
		return fmt.Errorf("%s token ID must be numeric: %w", forge, errs.ErrValidation)
	}

	if token.name != "lab-targets-"+runID {
		return fmt.Errorf("%s token name is not bound to run %q: %w", forge, runID, errs.ErrValidation)
	}

	if !rfc3339Pattern.MatchString(token.createdAt) {
		return fmt.Errorf("%s token creation time must be RFC3339: %w", forge, errs.ErrValidation)
	}

	createdAt, err := time.Parse(time.RFC3339, token.createdAt)
	if err != nil {
		return fmt.Errorf("%s token creation time: %w: %w", forge, err, errs.ErrValidation)
	}

	if createdAt.After(now.Add(allowedClockSkew)) {
		return fmt.Errorf("%s token creation time is in the future: %w", forge, errs.ErrValidation)
	}

	if token.expiresAt == "" {
		return validateRevocableToken(forge, runID, token, createdAt, now)
	}

	return validateExpiringToken(forge, token, now)
}

// validateRevocableToken covers the forges whose API cannot attach an expiry:
// the credential must instead be freshly minted and revocable with this run.
func validateRevocableToken(forge provider.ForgeAPI, runID string, token tokenMetadata, createdAt, now time.Time) error {
	{
		if createdAt.Before(now.Add(-maxUnexpiringTokenAge)) {
			return fmt.Errorf("%s non-expiring token is older than 24 hours: %w", forge, errs.ErrValidation)
		}

		if !token.revocationRequired || token.revocationRunID != runID {
			return fmt.Errorf("%s non-expiring token requires run-bound revocation metadata: %w", forge, errs.ErrValidation)
		}

		return nil
	}
}

// validateExpiringToken covers a natively expiring credential: the expiry must
// be real, soon, and not paired with revocation metadata that contradicts it.
func validateExpiringToken(forge provider.ForgeAPI, token tokenMetadata, now time.Time) error {
	if forge != provider.ForgeGitLab {
		return fmt.Errorf("%s has no native token expiry; revocation metadata is required: %w", forge, errs.ErrValidation)
	}

	expiry, err := parseExpiry(token.expiresAt)
	if err != nil {
		return fmt.Errorf("%s token expiry: %w: %w", forge, err, errs.ErrValidation)
	}

	if !expiry.After(now.Add(allowedClockSkew)) || expiry.After(now.Add(maximumNativeTokenExpiry)) {
		return fmt.Errorf("%s token expiry must be between 5 minutes and 31 days from now: %w", forge, errs.ErrValidation)
	}

	if token.revocationRequired || token.revocationRunID != "" {
		return fmt.Errorf("%s native expiry conflicts with revocation fallback metadata: %w", forge, errs.ErrValidation)
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

// selectedRefs reads the identity-bearing fields of every endpoint in the
// contract for which the operator declared an owner.
//
// Every endpoint the operator armed contributes, not only the one being acted
// on, and not only the kinds this suite drives. A credential the run can revoke
// is a credential the run is answerable for, so leaving a forge out of the
// identity would let one contract be silently reused against a provider the
// operator never confirmed.
func selectedRefs(lab labContract) ([]targetRef, error) {
	refs := make([]targetRef, 0, len(lab.Endpoints))

	seenKinds := make(map[string]struct{}, len(lab.Endpoints))
	for _, candidate := range lab.Endpoints {
		if _, seen := seenKinds[candidate.Kind]; seen {
			continue
		}

		seenKinds[candidate.Kind] = struct{}{}
		forge := provider.ForgeAPI(candidate.Kind)

		owner := ownerFor(forge)
		if owner == "" {
			continue
		}

		endpoint, err := selectedEndpointFor(lab, forge)
		if err != nil {
			return nil, err
		}

		host, err := endpoint.host()
		if err != nil {
			return nil, err
		}

		if err := validateDisposableHost(host); err != nil {
			return nil, err
		}

		refs = append(refs, targetRef{forge: endpoint.Kind, host: host, owner: owner})
	}

	return refs, nil
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
		if ref.forge == "" || seen[ref.forge] {
			return "", fmt.Errorf("selected providers are empty or repeated: %w", errs.ErrValidation)
		}

		seen[ref.forge] = true

		switch ref.forge {
		case string(provider.ForgeGitLab), string(provider.ForgeForgejo), endpointKindGitea:
		default:
			return "", fmt.Errorf("unknown provider %q is selected: %w", ref.forge, errs.ErrValidation)
		}

		if err := validateDisposableHost(ref.host); err != nil {
			return "", err
		}

		if !ownerPattern.MatchString(ref.owner) {
			return "", fmt.Errorf("%s owner %q is not a resource owner: %w", ref.forge, ref.owner, errs.ErrValidation)
		}

		entries = append(entries, identityEntry(ref.forge, ref.host, ref.owner, resourcePrefix))
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("no provider is selected: %w", errs.ErrValidation)
	}

	// Sorted so the identity is a property of what the run may touch, not of
	// the order the producer happened to write its endpoints in. Without this,
	// a producer reordering its contract would invalidate a confirmation the
	// operator had already typed for exactly the same set of targets, and the
	// shell preflight -- which reads the same file -- would have to reproduce
	// that ordering to agree.
	sort.Strings(entries)

	return "run=" + runID + "|targets=" + strings.Join(entries, ","), nil
}

func identityEntry(forge, host, owner, resourcePrefix string) string {
	return forge + "@https://" + host + "/" + owner + "#resources=" + resourcePrefix
}

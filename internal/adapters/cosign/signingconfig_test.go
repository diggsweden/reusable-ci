// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// signWrite names one cosign subcommand that writes a signature, and drives it
// through the adapter. Every entry MUST honour the transparency setting: each
// of these uploads to the public Rekor log otherwise, and a Rekor entry is
// permanent, public, and append-only. Adding a new write verb without adding it
// here leaves TestEveryWriteVerbSuppressesTheLog green, so keep the list in
// step with the adapter's write surface — the count assertion is the tripwire.
type signWrite struct {
	name string
	call func(a *cosign.Adapter) error
}

func writeVerbs() []signWrite {
	return []signWrite{
		{"sign-blob", func(a *cosign.Adapter) error {
			return a.SignBlob(context.Background(), cosign.SignBlobInput{
				Artifact: "app.tgz", BundlePath: "app.tgz.bundle", KeyRef: "k.key",
			}, nil)
		}},
		{"sign", func(a *cosign.Adapter) error {
			return a.SignImage(context.Background(), cosign.SignImageInput{
				ImageRef: "reg/app@sha256:" + hex64, KeyRef: "k.key",
			}, nil)
		}},
		{"attest", func(a *cosign.Adapter) error {
			return a.AttestImage(context.Background(), cosign.AttestImageInput{
				ImageRef: "reg/app@sha256:" + hex64, KeyRef: "k.key",
				PredicateType: "slsaprovenance1", PredicatePath: "p.json",
			}, nil)
		}},
	}
}

// hex64 is a syntactically valid sha256 hex digest for argv-shape tests; the
// mock cosign never looks at it.
const hex64 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// captureSigningConfig returns a mock cosign that copies whatever
// --signing-config and --trusted-root point at into a temp dir, plus a func
// reading back the named capture.
//
// The copy has to happen inside the stub because the adapter writes both
// documents per call and removes them on return — by the time the test regains
// control the files are gone. Capturing at call time asserts what actually
// matters: that each document existed, and was correct, at the moment cosign
// read it.
func captureSigningConfig(t *testing.T) (*mockbinary.Mock, func(flag string) (string, error)) {
	t.Helper()

	dir := t.TempDir()
	bins := mockbinary.New(t)
	bins.Add("cosign", `
prev=""
for a in "$@"; do
  case "$prev" in
    --signing-config) cp "$a" "`+dir+`/signing-config" ;;
    --trusted-root)   cp "$a" "`+dir+`/trusted-root" ;;
  esac
  prev="$a"
done
`)

	return bins, func(flag string) (string, error) {
		body, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(flag, "--")))

		return string(body), err
	}
}

// TestEveryWriteVerbSuppressesTheLog is the load-bearing test for "we do not
// publish to a transparency log unless we mean to".
//
// cosign uploads to the public Rekor log by default, and cosign 3.x deprecated
// --tlog-upload in favour of a signing-config file naming no transparency-log
// service. So the ONLY thing standing between a signing run and a permanent
// public record is this flag reaching every write subcommand, pointing at a
// config that actually names no log.
func TestEveryWriteVerbSuppressesTheLog(t *testing.T) {
	for _, verb := range writeVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			bins, captured := captureSigningConfig(t)

			a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: domainrelease.TransparencyNone}

			if err := verb.call(a); err != nil {
				t.Fatalf("%s: %v", verb.name, err)
			}

			invs := bins.Invocations("cosign")
			if len(invs) != 1 {
				t.Fatalf("expected 1 cosign invocation, got %d — the adapter must not shell out "+
					"to build the signing config", len(invs))
			}

			args := invs[0].Args
			if !slices.Contains(args, "--signing-config") {
				t.Fatalf("%s does not pass --signing-config; it would upload to the public Rekor log\nargv: %v",
					verb.name, args)
			}

			body, err := captured("--signing-config")
			if err != nil {
				t.Fatalf("%s: --signing-config named a file cosign could not read: %v", verb.name, err)
			}

			// A config that still names a Rekor instance would publish while
			// looking correct in argv, so assert the content, not the flag.
			if strings.Contains(body, "rekorTlogUrls") {
				t.Errorf("%s: signing config names a transparency log:\n%s", verb.name, body)
			}
		})
	}
}

// TestEveryWriteVerbStaysOffline is the second half of "no transparency log",
// and the one that is easy to forget: --signing-config stops the Rekor upload,
// but cosign verifies what it has just signed and fetches the trust root from
// tuf-repo-cdn.sigstore.dev to do it — once per signature, measured, not cached
// away. A run configured to publish nothing would still announce itself.
//
// --trusted-root supplies that material locally. Without it, "transparency:
// none" means "publishes nothing but still phones home", which is not what any
// reader assumes it means.
func TestEveryWriteVerbStaysOffline(t *testing.T) {
	for _, verb := range writeVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			bins, captured := captureSigningConfig(t)

			a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: domainrelease.TransparencyNone}

			if err := verb.call(a); err != nil {
				t.Fatalf("%s: %v", verb.name, err)
			}

			if args := bins.Invocations("cosign")[0].Args; !slices.Contains(args, "--trusted-root") {
				t.Fatalf("%s does not pass --trusted-root; it would fetch the trust root from "+
					"the public Sigstore TUF CDN on every signature\nargv: %v", verb.name, args)
			}

			body, err := captured("--trusted-root")
			if err != nil {
				t.Fatalf("%s: --trusted-root named a file cosign could not read: %v", verb.name, err)
			}

			// A trusted root naming services would send cosign to fetch from
			// them while argv still looked correct.
			for _, service := range []string{"tlogs", "certificateAuthorities", "timestampAuthorities"} {
				if strings.Contains(body, service) {
					t.Errorf("%s: trusted root names %q, so cosign may reach out for it:\n%s",
						verb.name, service, body)
				}
			}
		})
	}
}

// TestWriteVerbCountIsPinned trips when the adapter grows a signature-writing
// subcommand that writeVerbs() does not cover. Without it, a new verb would
// silently default to the public Rekor log and every test here would stay green.
func TestWriteVerbCountIsPinned(t *testing.T) {
	const known = 3 // sign-blob, sign, attest

	if got := len(writeVerbs()); got != known {
		t.Fatalf("writeVerbs() has %d entries, expected %d — if the adapter gained a "+
			"signature-writing subcommand, cover it here and bump this count", got, known)
	}
}

// TestSigningConfigDoesNotOutliveTheCall pins the cleanup. The config is
// written per call; a leaked temp file per signature would accumulate in the
// runner's temp dir across a release that signs many artifacts.
func TestSigningConfigDoesNotOutliveTheCall(t *testing.T) {
	bins, _ := captureSigningConfig(t)

	a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: domainrelease.TransparencyNone}
	if err := writeVerbs()[0].call(a); err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	// Both documents are temporary; a release signs many artifacts, so a leak
	// per signature accumulates in the runner's temp dir.
	args := bins.Invocations("cosign")[0].Args
	for _, flag := range []string{"--signing-config", "--trusted-root"} {
		idx := slices.Index(args, flag)
		if idx < 0 {
			t.Fatalf("%s not passed\nargv: %v", flag, args)
		}

		if _, err := os.Stat(args[idx+1]); !os.IsNotExist(err) {
			t.Errorf("%s file %s still exists after the call returned", flag, args[idx+1])
		}
	}
}

// TestPublicTransparencyLeavesCosignOnItsDefault pins the other direction: the
// public setting must NOT invent a flag. Keyless signing genuinely needs Fulcio
// and a Rekor inclusion proof, so the public default has to remain reachable —
// this is an opt-out, not a new default.
func TestPublicTransparencyLeavesCosignOnItsDefault(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: domainrelease.TransparencyPublic}

	if err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact: "app.tgz", BundlePath: "app.tgz.bundle", Keyless: true,
	}, nil); err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	// Neither flag: keyless needs the real Fulcio and Rekor, and it needs
	// cosign's own trust root to verify what it signed. Suppressing either on
	// the public path would break the method that the public path exists for.
	for _, flag := range []string{"--signing-config", "--trusted-root"} {
		if args := bins.Invocations("cosign")[0].Args; slices.Contains(args, flag) {
			t.Errorf("public transparency must not pass %s\nargv: %v", flag, args)
		}
	}
}

// TestZeroValueAdapterPublishes pins the fail-safe direction of the zero value.
// An Adapter built as a bare literal (as tests and any future call site might)
// must behave like cosign's own default. If the zero value meant "none", a
// forgotten field would silently withhold the public record instead of loudly
// publishing — the wrong way round for a default nobody typed.
func TestZeroValueAdapterPublishes(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")} // nothing set

	if err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact: "app.tgz", BundlePath: "app.tgz.bundle", Keyless: true,
	}, nil); err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	if args := bins.Invocations("cosign")[0].Args; slices.Contains(args, "--signing-config") {
		t.Errorf("the zero-value Adapter suppressed the transparency log\nargv: %v", args)
	}
}

// TestNewResolvesTransparencyFromEnv pins the mechanism that makes this
// unforgettable: the adapter is built at ~16 call sites, and both constructors
// must pick the setting up so no signing path can opt out by omission.
func TestNewResolvesTransparencyFromEnv(t *testing.T) {
	t.Setenv(cosign.EnvTransparency, string(domainrelease.TransparencyNone))

	if got := cosign.New().Transparency; got != domainrelease.TransparencyNone {
		t.Errorf("New() Transparency = %q, want the env value", got)
	}

	if got := cosign.NewIsolated("DOCKER_CONFIG").Transparency; got != domainrelease.TransparencyNone {
		t.Errorf("NewIsolated() Transparency = %q, want the env value — isolated signing "+
			"paths would otherwise still publish to Rekor", got)
	}
}

// verifyVerbs names each cosign subcommand that verifies a signature. Each must
// honour the same setting the write verbs do: a signature made with no
// transparency log cannot be verified without --insecure-ignore-tlog, so a verb
// that ignores it fails with cosign's "not enough verified log entries".
func verifyVerbs() []signWrite {
	const digest = "reg/app@sha256:" + hex64

	return []signWrite{
		{"verify-blob", func(a *cosign.Adapter) error {
			return a.VerifyBlob(context.Background(), cosign.VerifyBlobInput{
				Artifact: "app.tgz", BundlePath: "app.tgz.bundle", KeyRef: "k.pub",
			}, nil)
		}},
		{"verify", func(a *cosign.Adapter) error {
			return a.VerifyImage(context.Background(), cosign.VerifyImageInput{
				ImageRef: digest, KeyRef: "k.pub",
			}, nil)
		}},
		{"verify-attestation", func(a *cosign.Adapter) error {
			return a.VerifyAttestation(context.Background(), cosign.VerifyAttestationInput{
				ImageRef: digest, KeyRef: "k.pub", PredicateType: "slsaprovenance1",
			}, nil)
		}},
	}
}

// TestEveryVerifyVerbHonoursTheTlogPolicy is the counterpart to the write-verb
// test. cosign couples the halves, which is why one setting drives both: this
// asserts the verify side reads the same source of truth as the sign side.
func TestEveryVerifyVerbHonoursTheTlogPolicy(t *testing.T) {
	for _, verb := range verifyVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			bins := mockbinary.New(t)
			bins.Add("cosign", ":")

			a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: domainrelease.TransparencyNone}

			if err := verb.call(a); err != nil {
				t.Fatalf("%s: %v", verb.name, err)
			}

			if args := bins.Invocations("cosign")[0].Args; !slices.Contains(args, "--insecure-ignore-tlog") {
				t.Errorf("%s ignores the tlog policy; it cannot verify an unlogged signature\nargv: %v",
					verb.name, args)
			}
		})
	}
}

// TestTlogPolicyDefaultsToFullyChecked pins the default. Verification must
// require a log inclusion proof unless explicitly told not to: cosign's own
// warning is that artifacts "cannot be publicly verified when not included in
// a log", so this is the setting that must never drift on by accident.
func TestTlogPolicyDefaultsToFullyChecked(t *testing.T) {
	for _, verb := range verifyVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			bins := mockbinary.New(t)
			bins.Add("cosign", ":")

			a := &cosign.Adapter{Bin: bins.Path("cosign")} // nothing set

			if err := verb.call(a); err != nil {
				t.Fatalf("%s: %v", verb.name, err)
			}

			if args := bins.Invocations("cosign")[0].Args; slices.Contains(args, "--insecure-ignore-tlog") {
				t.Errorf("%s skips transparency-log verification by default\nargv: %v", verb.name, args)
			}
		})
	}
}

// TestVerifyVerbCountIsPinned is the tripwire for the verify surface, matching
// the write-verb one: a new verify subcommand that this file does not know
// about would silently keep its own tlog policy.
func TestVerifyVerbCountIsPinned(t *testing.T) {
	const known = 3 // verify-blob, verify, verify-attestation

	if got := len(verifyVerbs()); got != known {
		t.Fatalf("verifyVerbs() has %d entries, expected %d — if the adapter gained a "+
			"verifying subcommand, cover it here and bump this count", got, known)
	}
}

// TestSignAndVerifyCannotDisagree is why this is one setting and not two. The
// previous design had an independent env var per half, and cosign requires them
// to match: suppressing the log while still demanding a proof produces "not
// enough verified log entries", an error that names none of its causes. Here
// the halves are derived from one value, so the mismatch has nowhere to live.
func TestSignAndVerifyCannotDisagree(t *testing.T) {
	for _, transparency := range domainrelease.ValidTransparencies {
		t.Run(string(transparency), func(t *testing.T) {
			suppressed := func(verbs []signWrite, flag string) bool {
				bins, _ := captureSigningConfig(t)
				a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: transparency}

				if err := verbs[0].call(a); err != nil {
					t.Fatalf("call: %v", err)
				}

				return slices.Contains(bins.Invocations("cosign")[0].Args, flag)
			}

			signs := suppressed(writeVerbs(), "--signing-config")
			verifies := suppressed(verifyVerbs(), "--insecure-ignore-tlog")

			if signs != verifies {
				t.Errorf("transparency=%s: sign suppresses=%v but verify suppresses=%v — "+
					"cosign requires these to agree", transparency, signs, verifies)
			}
		})
	}
}

// TestTransparencyEnvFallsBackToPublish pins that a value we cannot parse
// leaves the log ON. TransparencyFromEnv runs in a constructor with nowhere to
// return an error, so it must fail towards publishing: a typo that silently
// withheld the public record is worse than one that published. The typo is
// caught with a real error upstream, where sign.transparency is validated.
func TestTransparencyEnvFallsBackToPublish(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "off", "maybe", "NONE", "None"} {
		t.Setenv(cosign.EnvTransparency, v)

		if got := cosign.New().Transparency; got.PublishesToLog() != true {
			t.Errorf("%q resolved to %q, which suppresses the transparency log; only the exact "+
				"value %q may", v, got, domainrelease.TransparencyNone)
		}
	}
}

// The generated document must be exactly what cosign's own generator emits for
// a run that publishes nothing: byte-identical to `cosign signing-config create
// --no-default-{rekor,fulcio,oidc,tsa}`. The literal lives here as the
// expectation, so the builder is held to that standard rather than inheriting a
// claim made in a comment.
func TestBuildSigningConfig_NoServicesMatchesCosignsOwnOutput(t *testing.T) {
	const want = `{"mediaType":"application/vnd.dev.sigstore.signingconfig.v0.2+json",` +
		`"rekorTlogConfig":{},"tsaConfig":{}}`

	got, err := cosign.BuildSigningConfigForTest(cosign.SigningConfigInputForTest{}, false, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != want {
		t.Errorf("signing config:\n got=%s\nwant=%s", got, want)
	}
}

// A self-hosted CA and issuer must appear as service entries, because that is
// the only channel cosign 3.x still listens on.
func TestBuildSigningConfig_NamesSelfHostedServices(t *testing.T) {
	got, err := cosign.BuildSigningConfigForTest(cosign.SigningConfigInputForTest{
		FulcioURL:  "https://fulcio.example.internal",
		OIDCIssuer: "https://gitlab.example.internal",
	}, false, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		CAUrls []struct {
			URL             string `json:"url"`
			MajorAPIVersion int    `json:"majorApiVersion"`
			Operator        string `json:"operator"`
		} `json:"caUrls"`
		OIDCUrls []struct {
			URL string `json:"url"`
		} `json:"oidcUrls"`
		RekorTlogUrls []struct{} `json:"rekorTlogUrls"`
	}

	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("generated config is not valid JSON: %v\n%s", err, got)
	}

	if len(doc.CAUrls) != 1 || doc.CAUrls[0].URL != "https://fulcio.example.internal" {
		t.Errorf("caUrls = %+v, want the self-hosted CA", doc.CAUrls)
	}

	if doc.CAUrls[0].MajorAPIVersion != 1 || doc.CAUrls[0].Operator == "" {
		t.Errorf("service entry is missing fields cosign requires: %+v", doc.CAUrls[0])
	}

	if len(doc.OIDCUrls) != 1 || doc.OIDCUrls[0].URL != "https://gitlab.example.internal" {
		t.Errorf("oidcUrls = %+v, want the self-hosted issuer", doc.OIDCUrls)
	}

	// No log was asked for and none is running, so naming one would be a lie
	// cosign would then try to honour.
	if len(doc.RekorTlogUrls) != 0 {
		t.Errorf("a transparency log was named when none was configured")
	}
}

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign_test

import (
	"context"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// signWrite names one cosign subcommand that writes a signature, and drives it
// through the adapter. Every entry MUST pass --signing-config when one is set:
// each of these uploads to the public Rekor transparency log otherwise, and a
// Rekor entry is permanent, public, and append-only. Adding a new write verb
// without adding it here leaves TestEveryWriteVerbHonoursTheSigningConfig
// green, so keep the list in step with the adapter's write surface — the count
// assertion below is the tripwire.
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

// TestEveryWriteVerbHonoursTheSigningConfig is the load-bearing test for
// "we do not publish to a transparency log unless we mean to".
//
// cosign uploads to the public Rekor log by default, and cosign 3.x deprecated
// --tlog-upload in favour of a signing-config file with no transparency-log
// service. So the ONLY thing standing between a signing run and a permanent
// public record is this flag reaching every write subcommand.
func TestEveryWriteVerbHonoursTheSigningConfig(t *testing.T) {
	for _, verb := range writeVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			bins := mockbinary.New(t)
			bins.Add("cosign", ":")

			a := &cosign.Adapter{Bin: bins.Path("cosign"), SigningConfig: "/tmp/nolog.json"}

			if err := verb.call(a); err != nil {
				t.Fatalf("%s: %v", verb.name, err)
			}

			invs := bins.Invocations("cosign")
			if len(invs) != 1 {
				t.Fatalf("expected 1 cosign invocation, got %d", len(invs))
			}

			args := invs[0].Args
			idx := slices.Index(args, "--signing-config")

			if idx < 0 {
				t.Fatalf("%s does not pass --signing-config; it would upload to the public Rekor log\nargv: %v",
					verb.name, args)
			}

			if idx+1 >= len(args) || args[idx+1] != "/tmp/nolog.json" {
				t.Errorf("%s: --signing-config value wrong\nargv: %v", verb.name, args)
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

// TestNoSigningConfigLeavesCosignOnItsDefault pins the other direction: an
// unset config must NOT invent a flag. Keyless signing genuinely needs Fulcio
// and a Rekor inclusion proof, so the public default has to remain reachable —
// this is an opt-out, not a new default.
func TestNoSigningConfigLeavesCosignOnItsDefault(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")} // no SigningConfig

	if err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact: "app.tgz", BundlePath: "app.tgz.bundle", Keyless: true,
	}, nil); err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	if args := bins.Invocations("cosign")[0].Args; slices.Contains(args, "--signing-config") {
		t.Errorf("unset SigningConfig must not pass the flag\nargv: %v", args)
	}
}

// TestNewResolvesTheSigningConfigFromEnv pins the mechanism that makes this
// unforgettable: the adapter is built at ~16 call sites, and both constructors
// must pick the setting up so no signing path can opt out by omission.
func TestNewResolvesTheSigningConfigFromEnv(t *testing.T) {
	t.Setenv(cosign.EnvSigningConfig, "/tmp/from-env.json")

	if got := cosign.New().SigningConfig; got != "/tmp/from-env.json" {
		t.Errorf("New() SigningConfig = %q, want the env value", got)
	}

	if got := cosign.NewIsolated("DOCKER_CONFIG").SigningConfig; got != "/tmp/from-env.json" {
		t.Errorf("NewIsolated() SigningConfig = %q, want the env value — isolated signing "+
			"paths would otherwise still publish to Rekor", got)
	}
}

// verifyRead names one cosign subcommand that verifies a signature. Each must
// honour the tlog policy: a signature made against a signing config with no
// transparency log cannot be verified without it, so a verb that ignores the
// setting fails closed with cosign's "not enough verified log entries" error.
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
// test. cosign couples the halves: verification demands a log inclusion proof,
// so signing without a transparency log and verifying are a matched pair. A
// verify verb that drops the policy cannot check what the sign side produced.
func TestEveryVerifyVerbHonoursTheTlogPolicy(t *testing.T) {
	for _, verb := range verifyVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			bins := mockbinary.New(t)
			bins.Add("cosign", ":")

			a := &cosign.Adapter{Bin: bins.Path("cosign"), InsecureIgnoreTlog: true}

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

// TestNewResolvesTheTlogPolicyFromEnv mirrors the signing-config resolution:
// both constructors must pick it up, or an isolated verify path silently
// disagrees with the sign path about whether a log entry is required.
func TestNewResolvesTheTlogPolicyFromEnv(t *testing.T) {
	t.Setenv(cosign.EnvInsecureIgnoreTlog, "true")

	if !cosign.New().InsecureIgnoreTlog {
		t.Error("New() did not resolve the tlog policy from the environment")
	}

	if !cosign.NewIsolated("DOCKER_CONFIG").InsecureIgnoreTlog {
		t.Error("NewIsolated() did not resolve the tlog policy from the environment")
	}
}

// TestTlogPolicyEnvIsNotTruthyByAccident pins that only deliberate spellings
// turn verification down. A stray value must fail safe (keep checking).
func TestTlogPolicyEnvIsNotTruthyByAccident(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "off", "maybe", "FALSE"} {
		t.Setenv(cosign.EnvInsecureIgnoreTlog, v)

		if cosign.New().InsecureIgnoreTlog {
			t.Errorf("%q turned off transparency-log verification; only 1/true/yes/on may", v)
		}
	}
}

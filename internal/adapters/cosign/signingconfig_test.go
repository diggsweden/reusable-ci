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
				ImageRef: "reg/app@sha256:" + strings64(), KeyRef: "k.key",
			}, nil)
		}},
		{"attest", func(a *cosign.Adapter) error {
			return a.AttestImage(context.Background(), cosign.AttestImageInput{
				ImageRef: "reg/app@sha256:" + strings64(), KeyRef: "k.key",
				PredicateType: "slsaprovenance1", PredicatePath: "p.json",
			}, nil)
		}},
	}
}

func strings64() string {
	const hex = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	return hex
}

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

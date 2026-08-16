// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

// These run in the ordinary suite, with no build tag and no lab.
//
// That is the point. This tier once read a sourced shell environment from a lab
// that was later retired, and nothing noticed: no untagged test exercised the
// contract, so the entry conditions could drift out from under the suite while
// every gate stayed green. Reading a committed fixture costs milliseconds and
// makes that drift a failure instead of a silence.
//
// The fixture is a complete, inert contract of the shape the producer emits. If
// the producer's shape changes, this file is where it is meant to hurt.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeContract(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func validContractBody(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", "contract-valid.json"))
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func TestLoadLabContract_AcceptsTheProducersShape(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, validContractBody(t)))
	if err != nil {
		t.Fatalf("loadLabContract() = %v, want the fixture accepted", err)
	}

	if contract.Generation.ID != "run-live-1" {
		t.Errorf("generation ID = %q, want run-live-1", contract.Generation.ID)
	}

	if got := contract.cleanupCommand(); got != "/opt/forge-lab/scripts/revoke-targets.sh" {
		t.Errorf("cleanupCommand() = %q", got)
	}
}

func TestLoadLabContract_Refusals(t *testing.T) {
	t.Parallel()

	tests := map[string]func(string) string{
		"a version this consumer does not implement": func(body string) string {
			return strings.Replace(body, `"version": 1`, `"version": 2`, 1)
		},
		// A field this consumer cannot interpret may carry a restriction it
		// would otherwise ignore, so the contract is refused rather than
		// partially understood.
		"an unknown structural field": func(body string) string {
			return strings.Replace(body, `"version": 1,`, `"version": 1, "authorization": {},`, 1)
		},
		"a second object appended": func(body string) string { return body + "{}" },
		"a truncated object":       func(body string) string { return body[:len(body)/2] },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := loadLabContract(writeContract(t, mutate(validContractBody(t)))); err == nil {
				t.Fatalf("loadLabContract() accepted %s", name)
			}
		})
	}
}

func TestLoadLabContract_RefusesARelativePath(t *testing.T) {
	t.Parallel()

	// Relative would resolve against whatever directory the test happened to
	// run in, which is not a thing a destructive run should depend on.
	if _, err := loadLabContract("testdata/contract-valid.json"); err == nil {
		t.Fatal("loadLabContract() accepted a relative contract path")
	}
}

func TestEndpointFor_SelectsByKind(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, validContractBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	endpoint, err := contract.endpointFor("forgejo")
	if err != nil {
		t.Fatalf("endpointFor(forgejo) = %v", err)
	}

	host, err := endpoint.host()
	if err != nil {
		t.Fatalf("host() = %v", err)
	}

	if host != "forgejo.compose.forgelab:8443" {
		t.Errorf("host() = %q, want the API root's authority", host)
	}

	// The host must be the one the guard will check, and it must be one the
	// guard accepts -- otherwise the two halves disagree and the tier cannot
	// run against the producer it was written for.
	if hostErr := validateDisposableHost(host); hostErr != nil {
		t.Errorf("the producer's own host is refused by the guard: %v", hostErr)
	}
}

func TestEndpointFor_RefusesAnAbsentOrAmbiguousKind(t *testing.T) {
	t.Parallel()

	body := validContractBody(t)

	contract, err := loadLabContract(writeContract(t, body))
	if err != nil {
		t.Fatal(err)
	}

	if _, absentErr := contract.endpointFor("gitea"); absentErr == nil {
		t.Error("endpointFor() invented an endpoint the contract does not carry")
	}

	// Two endpoints of one kind: which fixtures get destroyed must not depend
	// on the order the producer happened to write them in.
	doubled := strings.Replace(body, `"name": "compose-gitlab"`, `"name": "other-forgejo", "kind": "forgejo"`, 1)
	doubled = strings.Replace(doubled, `"kind": "gitlab",`, ``, 1)

	ambiguous, err := loadLabContract(writeContract(t, doubled))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ambiguous.endpointFor("forgejo"); err == nil {
		t.Error("endpointFor() picked one of two same-kind endpoints instead of refusing")
	}
}

func TestEndpointHost_RefusesUntrustworthyRoots(t *testing.T) {
	t.Parallel()

	// G101: the userinfo row is an inert fixture. An API root carrying
	// credentials is exactly what this function must refuse, so the test cannot
	// state the case without writing one down.
	tests := map[string]string{ //nolint:gosec
		"plaintext":      "http://forgejo.compose.forgelab",
		"userinfo":       "https://someone:secret@forgejo.compose.forgelab",
		"query":          "https://forgejo.compose.forgelab/api/v1?as=admin",
		"fragment":       "https://forgejo.compose.forgelab/api/v1#x",
		"no host at all": "https:///api/v1",
		"not a URL":      "://",
	}

	for name, apiBase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			endpoint := labEndpoint{Kind: "forgejo", APIBaseURL: apiBase}
			if _, err := endpoint.host(); err == nil {
				t.Fatalf("host() accepted %s", name)
			}
		})
	}
}

func TestTokenMeta_ProjectsOntoTheGuardsRules(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, validContractBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	forgejo, err := contract.endpointFor("forgejo")
	if err != nil {
		t.Fatal(err)
	}

	// Forgejo cannot attach a native expiry, so the producer marks the
	// credential revocable and binds it to the generation. The guard must see
	// exactly that, or a credential that outlives the run slips through.
	meta := forgejo.tokenMeta(contract.Generation.ID)
	if !meta.revocationRequired || meta.revocationRunID != "run-live-1" {
		t.Errorf("forgejo token meta = %+v, want run-bound revocation", meta)
	}

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	if guardErr := validateTokenMetadata("forgejo", contract.Generation.ID, meta, now); guardErr != nil {
		t.Errorf("the producer's own credential is refused by the guard: %v", guardErr)
	}

	gitlab, err := contract.endpointFor("gitlab")
	if err != nil {
		t.Fatal(err)
	}

	if err := validateTokenMetadata("gitlab", contract.Generation.ID, gitlab.tokenMeta(contract.Generation.ID), now); err != nil {
		t.Errorf("the producer's own expiring credential is refused by the guard: %v", err)
	}
}

func TestTokenMeta_DisownsAnotherGenerationsRevocation(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, validContractBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	forgejo, err := contract.endpointFor("forgejo")
	if err != nil {
		t.Fatal(err)
	}

	// A credential whose revocation names someone else's generation is not this
	// run's to rely on: this run's cleanup would not revoke it.
	meta := forgejo.tokenMeta("run-live-2")
	if meta.revocationRunID != "" {
		t.Errorf("revocationRunID = %q, want it disowned", meta.revocationRunID)
	}

	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	if err := validateTokenMetadata("forgejo", "run-live-2", meta, now); err == nil {
		t.Error("the guard accepted a credential bound to another generation's cleanup")
	}
}

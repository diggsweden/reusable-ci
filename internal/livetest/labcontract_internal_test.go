// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func writeContract(t *testing.T, body string) string {
	t.Helper()

	return writeContractBytes(t, []byte(body))
}

func writeContractBytes(t *testing.T, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func setCurrentContract(t *testing.T, path string) {
	t.Helper()

	_, facts, err := readPrivateContractBound(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(contractFileEnv, path)
	t.Setenv(frozenContractFactsEnv, facts)
}

func TestLoadLabContract_RejectsCaseVariantsAtEveryObjectBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, from, to string }{
		{name: "root", from: `"version": 2`, to: `"Version": 2`},
		{name: "generation", from: `{"id": "run-neutral-fixture"}`, to: `{"ID": "run-neutral-fixture"}`},
		{name: "endpoint", from: `"kind": "forgejo"`, to: `"Kind": "forgejo"`},
		{name: "oci_registry", from: `"credential": "endpoint"`, to: `"Credential": "endpoint"`},
		{name: "credential", from: `"username": "fixture-user"`, to: `"Username": "fixture-user"`},
		{name: "revocation", from: `"required": true`, to: `"Required": true`},
		{name: "fulcio", from: `"issuers": [`, to: `"Issuers": [`},
		{name: "fulcio_issuer", from: `"endpoint": "forgejo"`, to: `"Endpoint": "forgejo"`},
		{name: "interfaces", from: `"credential_cleanup": {`, to: `"Credential_Cleanup": {`},
		{name: "cleanup", from: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env"`, to: `"Contract_File": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env"`},
		{name: "extended_fixture", from: `"protocol": "shape-v2"`, to: `"Protocol": "shape-v2"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body := strings.Replace(composeFixtureBody(t), testCase.from, testCase.to, 1)
			if _, err := loadLabContract(writeContract(t, body)); err == nil {
				t.Fatalf("accepted case variant at %s boundary", testCase.name)
			}
		})
	}
}

func TestLoadLabContract_RejectsDuplicateKeysAtEveryObjectBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, from, to string }{
		{name: "root", from: `"version": 2,`, to: `"version": 2, "version": 2,`},
		{name: "generation", from: `{"id": "run-neutral-fixture"}`, to: `{"id": "run-neutral-fixture", "id": "run-neutral-fixture"}`},
		{name: "endpoint", from: `"name": "forgejo",`, to: `"name": "forgejo", "name": "forgejo",`},
		{name: "oci_registry", from: `"credential": "endpoint"`, to: `"credential": "endpoint", "credential": "endpoint"`},
		{name: "credential", from: `"username": "fixture-user",`, to: `"username": "fixture-user", "username": "fixture-user",`},
		{name: "revocation", from: `{"required": true, "generation_id": "run-neutral-fixture"}`, to: `{"required": true, "required": true, "generation_id": "run-neutral-fixture"}`},
		{name: "fulcio", from: `"base_url": "https://fulcio.compose.forgelab:8443",`, to: `"base_url": "https://fulcio.compose.forgelab:8443", "base_url": "https://fulcio.compose.forgelab:8443",`},
		{name: "fulcio_issuer", from: `"endpoint": "forgejo",`, to: `"endpoint": "forgejo", "endpoint": "forgejo",`},
		{name: "interfaces", from: `"credential_cleanup": {`, to: `"credential_cleanup": null, "credential_cleanup": {`},
		{name: "cleanup", from: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env"`, to: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env", "contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env"`},
		{name: "extended_fixture", from: `"protocol": "shape-v2"`, to: `"protocol": "shape-v2", "protocol": "shape-v2"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body := strings.Replace(composeFixtureBody(t), testCase.from, testCase.to, 1)
			if _, err := loadLabContract(writeContract(t, body)); err == nil || !strings.Contains(err.Error(), "duplicate object key") {
				t.Fatalf("duplicate key at %s boundary result = %v", testCase.name, err)
			}
		})
	}
}

func TestLoadLabContract_RejectsRequiredBooleanOmittedOrNull(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, from, to string }{
		{name: "omitted", from: `"required": true, `, to: ``},
		{name: "null", from: `"required": true`, to: `"required": null`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body := strings.Replace(composeFixtureBody(t), testCase.from, testCase.to, 1)
			if _, err := loadLabContract(writeContract(t, body)); err == nil {
				t.Fatalf("accepted revocation.required %s", testCase.name)
			}
		})
	}
}

func TestLoadLabContract_RejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	body := []byte(composeFixtureBody(t))
	marker := []byte("fixture-user")
	index := bytes.Index(body, marker)

	body[index] = 0xff
	if _, err := loadLabContract(writeContractBytes(t, body)); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("invalid UTF-8 result = %v", err)
	}
}

func fixtureBody(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func composeFixtureBody(t *testing.T) string {
	t.Helper()

	return fixtureBody(t, "neutral-targets-v2-compose.json")
}

func TestLoadLabContract_V2CanonicalFixtures(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"neutral-targets-v2-compose.json", "neutral-targets-v2-github.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			contract, err := loadLabContract(writeContract(t, fixtureBody(t, name)))
			if err != nil {
				t.Fatalf("loadLabContract() rejected canonical fixture: %v", err)
			}

			if contract.Version != 2 || !contract.CAFile.Set || !contract.Fulcio.Set {
				t.Fatalf("required v2 projection is incomplete: %+v", contract)
			}
		})
	}
}

func TestCanonicalFixtures_MatchRecordedProducerHashes(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"neutral-targets-v2-compose.json": "9a51e568819087cc62779e790f3c214b714817755fa2e80fec90a7ee6bcb310a",
		"neutral-targets-v2-github.json":  "5ff2f640446ad635e94ec05365079432189be78419eb5f7208198e99b5590910",
	}
	for name, wantHash := range want {
		body := []byte(fixtureBody(t, name))
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != wantHash {
			t.Errorf("%s hash = %s, want canonical producer hash %s", name, got, wantHash)
		}
	}
}

func TestLoadLabContract_V1DebtIsRejectedExplicitly(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		strings.Replace(composeFixtureBody(t), `"version": 2`, `"version": 1`, 1),
		`{"version":1,"interfaces":{"credential_cleanup":"/old/v1/cleanup"}}`,
	} {
		_, err := loadLabContract(writeContract(t, body))
		if err == nil || !strings.Contains(err.Error(), "version 1 is retired") {
			t.Fatalf("v1 rejection = %v, want explicit retired-version error", err)
		}
	}
}

func TestLoadLabContract_V2VersionAndStructuralRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{name: "version_3", mutate: func(body string) string { return strings.Replace(body, `"version": 2`, `"version": 3`, 1) }},
		{name: "unknown_root", mutate: func(body string) string {
			return strings.Replace(body, `"version": 2,`, `"version": 2, "authorization": {},`, 1)
		}},
		{name: "unknown_nested_registry", mutate: func(body string) string {
			return strings.Replace(body, `"credential": "endpoint"`, `"credential": "endpoint", "scope": "push"`, 1)
		}},
		{name: "unknown_nested_fulcio", mutate: func(body string) string {
			return strings.Replace(body, `"issuers": [`, `"trust_root": "x", "issuers": [`, 1)
		}},
		{name: "unknown_nested_cleanup", mutate: func(body string) string {
			return strings.Replace(body, `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env"`, `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env", "argv": []`, 1)
		}},
		{name: "duplicate_root", mutate: func(body string) string {
			return strings.Replace(body, `"version": 2,`, `"version": 2, "version": 2,`, 1)
		}},
		{name: "duplicate_nested", mutate: func(body string) string {
			return strings.Replace(body, `"credential": "endpoint"`, `"credential": "endpoint", "credential": "endpoint"`, 1)
		}},
		{name: "trailing_document", mutate: func(body string) string { return body + `{}` }},
		{name: "truncated", mutate: func(body string) string { return body[:len(body)/2] }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := loadLabContract(writeContract(t, testCase.mutate(composeFixtureBody(t)))); err == nil {
				t.Fatalf("loadLabContract() accepted %s", testCase.name)
			}
		})
	}
}

func TestLoadLabContract_V2RequiredNullableDistinctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{name: "missing_root_fulcio", mutate: func(body string) string {
			start := strings.Index(body, `  "fulcio": {`)
			end := strings.Index(body[start:], `  "interfaces":`)

			return body[:start] + body[start+end:]
		}},
		{name: "missing_root_ca_file", mutate: func(body string) string {
			return strings.Replace(body, `  "ca_file": "/opt/forge-lab/certs/forge-lab-ca.crt",
`, "", 1)
		}},
		{name: "missing_oci_registry", mutate: func(body string) string {
			start := strings.Index(body, `      "oci_registry": {`)
			end := strings.Index(body[start:], `      "credential": {`)

			return body[:start] + body[start+end:]
		}},
		{name: "null_credential", mutate: func(body string) string {
			start := strings.Index(body, `      "credential": {`)
			end := strings.Index(body[start:], "\n      }")

			return body[:start] + `      "credential": null` + body[start+end+8:]
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := loadLabContract(writeContract(t, testCase.mutate(composeFixtureBody(t)))); err == nil {
				t.Fatalf("loadLabContract() accepted %s", testCase.name)
			}
		})
	}

	github, err := loadLabContract(writeContract(t, fixtureBody(t, "neutral-targets-v2-github.json")))
	if err != nil || github.CAFile.Value != nil || github.Fulcio.Value != nil {
		t.Fatalf("required explicit nulls were not accepted: contract=%+v err=%v", github, err)
	}

	withNullRegistry := strings.Replace(composeFixtureBody(t), `"oci_registry": {
        "base_url": "https://forgejo.compose.forgelab:8443",
        "credential": "endpoint"
      }`, `"oci_registry": null`, 1)
	if _, err := loadLabContract(writeContract(t, withNullRegistry)); err != nil {
		t.Fatalf("required explicit null oci_registry was rejected: %v", err)
	}
}

func TestLoadLabContract_V2OptionalFieldsMayBeOmittedOrNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{name: "interfaces_omitted", mutate: func(body string) string {
			start := strings.Index(body, `  "interfaces": {`)

			return strings.TrimSuffix(body[:start], ",\n") + "\n}\n"
		}},
		{name: "interfaces_null", mutate: func(body string) string {
			start := strings.Index(body, `  "interfaces": {`)

			return body[:start] + `  "interfaces": null
}
`
		}},
		{name: "git_base_url_omitted", mutate: func(body string) string {
			return strings.Replace(body, `      "git_base_url": "https://forgejo.compose.forgelab:8443",
`, "", 1)
		}},
		{name: "git_base_url_null", mutate: func(body string) string {
			return strings.Replace(body, `"git_base_url": "https://forgejo.compose.forgelab:8443"`, `"git_base_url": null`, 1)
		}},
		{name: "provider_id_omitted", mutate: func(body string) string {
			return strings.Replace(body, `        "provider_id": "101",
`, "", 1)
		}},
		{name: "expires_at_omitted", mutate: func(body string) string {
			return strings.Replace(body, `        "expires_at": null,
`, "", 1)
		}},
		{name: "expiring_revocation_generation_omitted", mutate: func(body string) string {
			return strings.Replace(body, `"revocation": {"required": false, "generation_id": null}`, `"revocation": {"required": false}`, 1)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if _, err := loadLabContract(writeContract(t, testCase.mutate(composeFixtureBody(t)))); err != nil {
				t.Fatalf("optional v2 field form was rejected: %v", err)
			}
		})
	}
}

func TestLoadLabContract_NonExpiringRevocationRequiresGeneration(t *testing.T) {
	t.Parallel()

	body := strings.Replace(composeFixtureBody(t),
		`"revocation": {"required": true, "generation_id": "run-neutral-fixture"}`,
		`"revocation": {"required": true}`, 1)
	if _, err := loadLabContract(writeContract(t, body)); err == nil || !strings.Contains(err.Error(), "not bound to generation") {
		t.Fatalf("non-expiring credential without generation result = %v", err)
	}
}

func TestLoadLabContract_CapabilitiesAreASetAfterWireParsing(t *testing.T) {
	t.Parallel()

	body := strings.Replace(composeFixtureBody(t),
		`"capabilities": ["artifacts", "organizations", "packages", "repositories", "workflow-runs"]`,
		`"capabilities": ["artifacts", "organizations", "artifacts", "packages", "repositories", "workflow-runs", "packages"]`, 1)
	contract, err := loadLabContract(writeContract(t, body))
	if err != nil {
		t.Fatalf("wire parser rejected duplicate non-empty capabilities: %v", err)
	}

	endpoint, err := contract.endpointFor("forgejo", "")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"artifacts", "organizations", "packages", "repositories", "workflow-runs"}
	if !reflect.DeepEqual(endpoint.Capabilities, want) {
		t.Fatalf("normalized capabilities = %v, want %v", endpoint.Capabilities, want)
	}
	if err := endpoint.requireLiveCapabilities(); err != nil || !endpoint.hasCapability("artifacts") {
		t.Fatalf("set-like capability consumers rejected normalized capabilities: %v", err)
	}
}

func TestLoadLabContract_RejectsEmptyCapability(t *testing.T) {
	t.Parallel()

	body := strings.Replace(composeFixtureBody(t),
		`"capabilities": ["artifacts",`, `"capabilities": ["", "artifacts",`, 1)
	if _, err := loadLabContract(writeContract(t, body)); err == nil || !strings.Contains(err.Error(), "empty capability") {
		t.Fatalf("empty capability result = %v", err)
	}
}

func TestEndpointFor_DuplicateKindsRequireExactOperatorSelection(t *testing.T) {
	t.Parallel()

	body := strings.Replace(composeFixtureBody(t), `"kind": "gitea"`, `"kind": "forgejo"`, 1)

	contract, err := loadLabContract(writeContract(t, body))
	if err != nil {
		t.Fatalf("wire parser rejected duplicate endpoint kinds: %v", err)
	}

	if _, selectionErr := contract.endpointFor("forgejo", ""); selectionErr == nil ||
		!strings.Contains(selectionErr.Error(), "RC_LIVE_FORGEJO_ENDPOINT") {
		t.Fatalf("ambiguous kind result = %v", selectionErr)
	}

	endpoint, err := contract.endpointFor("forgejo", "gitea")
	if err != nil || endpoint.Name != "gitea" {
		t.Fatalf("exact endpoint selection = %+v, %v", endpoint, err)
	}

	if _, selectionErr := contract.endpointFor("forgejo", "missing"); selectionErr == nil {
		t.Fatal("endpoint selection invented a missing exact name")
	}
}

func TestLoadLabContract_V2URLGrammar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from string
		to   string
	}{
		{name: "backslash", from: `https://forgejo.compose.forgelab:8443/api/v1`, to: `https://forgejo.compose.forgelab:8443\\api\\v1`},
		{name: "userinfo", from: `https://forgejo.compose.forgelab:8443/api/v1`, to: `https://user@forgejo.compose.forgelab:8443/api/v1`},
		{name: "unsafe_port", from: `https://forgejo.compose.forgelab:8443/api/v1`, to: `https://forgejo.compose.forgelab:65536/api/v1`},
		{name: "query", from: `https://forgejo.compose.forgelab:8443/api/v1`, to: `https://forgejo.compose.forgelab:8443/api/v1?admin=1`},
		{name: "fragment", from: `https://forgejo.compose.forgelab:8443/api/v1`, to: `https://forgejo.compose.forgelab:8443/api/v1#x`},
		{name: "oci_path", from: `"base_url": "https://forgejo.compose.forgelab:8443"`, to: `"base_url": "https://forgejo.compose.forgelab:8443/v2"`},
		{name: "fulcio_path", from: `https://fulcio.compose.forgelab:8443",`, to: `https://fulcio.compose.forgelab:8443/api",`},
		{name: "gitea_registry_not_forge_origin", from: `"base_url": "https://gitea.compose.forgelab:8443"`, to: `"base_url": "https://registry.gitea.compose.forgelab:8443"`},
		{name: "unknown_fulcio_endpoint_reference", from: `"endpoint": "forgejo"`, to: `"endpoint": "missing"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body := strings.Replace(composeFixtureBody(t), testCase.from, testCase.to, 1)
			if _, err := loadLabContract(writeContract(t, body)); err == nil {
				t.Fatalf("loadLabContract() accepted %s", testCase.name)
			}
		})
	}
}

func TestLoadLabContract_V2ProjectsProducerFacts(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, composeFixtureBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	forgejo, err := contract.endpointFor("forgejo", "")
	if err != nil {
		t.Fatal(err)
	}

	if forgejo.registryOrigin() != "https://forgejo.compose.forgelab:8443" || len(forgejo.Capabilities) != 5 {
		t.Fatalf("forgejo projection = %+v", forgejo)
	}

	fulcioURL, issuer, ok := contract.fulcioFor("forgejo")
	if !ok || fulcioURL != "https://fulcio.compose.forgelab:8443" || issuer != "https://forgejo.compose.forgelab:8443/api/actions" {
		t.Fatalf("Fulcio projection = %q %q %v", fulcioURL, issuer, ok)
	}

	forgeAuthorities, err := forgejo.forgeAuthorities()
	if err != nil {
		t.Fatal(err)
	}

	registryAuthorities, err := forgejo.registryAuthorities()
	if err != nil {
		t.Fatal(err)
	}

	oidcAllowed, err := oidcAuthorities(fulcioURL)
	if err != nil {
		t.Fatal(err)
	}

	forgeOrigin := []string{"https://forgejo.compose.forgelab:8443"}

	fulcioOrigin := []string{"https://fulcio.compose.forgelab:8443"}
	if !reflect.DeepEqual(forgeAuthorities, forgeOrigin) ||
		!reflect.DeepEqual(registryAuthorities, forgeOrigin) ||
		!reflect.DeepEqual(oidcAllowed, fulcioOrigin) {
		t.Fatalf("authority classes: forge=%v registry=%v OIDC=%v", forgeAuthorities, registryAuthorities, oidcAllowed)
	}

	gitlab, err := contract.endpointFor("gitlab", "")
	if err != nil {
		t.Fatal(err)
	}

	gitlabRegistryAuthorities, err := gitlab.registryAuthorities()
	if err != nil {
		t.Fatal(err)
	}

	wantGitLabRegistryAuthorities := []string{
		"https://registry.gitlab.compose.forgelab:8443",
		"https://gitlab.compose.forgelab:8443",
	}
	if !reflect.DeepEqual(gitlabRegistryAuthorities, wantGitLabRegistryAuthorities) {
		t.Fatalf("GitLab registry authorities = %v, want %v", gitlabRegistryAuthorities, wantGitLabRegistryAuthorities)
	}

	command, contractFile := contract.cleanupPair()
	if command != "/home/garga/.local/state/forge-lab/target-cleanup-run-neutral-fixture/cleanup" ||
		contractFile != "/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env" {
		t.Fatalf("cleanup pair = %q %q", command, contractFile)
	}

	if contract.Interfaces.ExtendedFixtureProducer.Protocol != "shape-v2" {
		t.Fatalf("extended fixture projection = %+v", contract.Interfaces.ExtendedFixtureProducer)
	}
}

func TestLoadLabContract_FileSafety(t *testing.T) {
	t.Parallel()
	body := composeFixtureBody(t)

	t.Run("relative", func(t *testing.T) {
		if _, err := loadLabContract("testdata/neutral-targets-v2-compose.json"); err == nil {
			t.Fatal("accepted relative path")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()

		realPath := filepath.Join(dir, "real.json")
		if err := os.WriteFile(realPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		link := filepath.Join(dir, "link.json")
		if err := os.Symlink(realPath, link); err != nil {
			t.Fatal(err)
		}

		if _, err := loadLabContract(link); err == nil {
			t.Fatal("accepted symlink")
		}
	})
	t.Run("symlink_parent", func(t *testing.T) {
		root := t.TempDir()

		realParent := filepath.Join(root, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}

		realPath := filepath.Join(realParent, "contract.json")
		if err := os.WriteFile(realPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		linkedParent := filepath.Join(root, "linked")
		if err := os.Symlink(realParent, linkedParent); err != nil {
			t.Fatal(err)
		}

		if _, err := loadLabContract(filepath.Join(linkedParent, "contract.json")); err == nil {
			t.Fatal("accepted contract through a symlinked parent")
		}
	})
	t.Run("permissions", func(t *testing.T) {
		path := writeContract(t, body)
		if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // Deliberately unsafe mode must be rejected.
			t.Fatal(err)
		}

		if _, err := loadLabContract(path); err == nil {
			t.Fatal("accepted group/world-readable contract")
		}
	})
	t.Run("size", func(t *testing.T) {
		if _, err := loadLabContract(writeContract(t, strings.Repeat(" ", maxContractBytes+1))); err == nil {
			t.Fatal("accepted oversized contract")
		}
	})
}

func TestCurrentLabContract_DetectsMutationAfterFirstLoad(t *testing.T) {
	body := composeFixtureBody(t)
	path := writeContract(t, body)
	setCurrentContract(t, path)

	if _, err := currentLabContract(); err != nil {
		t.Fatal(err)
	}

	changed := strings.Replace(body, `"run-neutral-fixture"`, `"run-mutated-fixture"`, 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := currentLabContract(); err == nil || !strings.Contains(err.Error(), "does not match preflight facts") {
		t.Fatalf("mutation result = %v", err)
	}
}

func TestCurrentLabContract_RejectsReplacementBeforeFirstLoad(t *testing.T) {
	body := composeFixtureBody(t)
	path := writeContract(t, body)
	setCurrentContract(t, path)

	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := currentLabContract(); err == nil || !strings.Contains(err.Error(), "does not match preflight facts") {
		t.Fatalf("replacement result = %v", err)
	}
}

func TestSelected_MalformedContractCannotBecomeASkip(t *testing.T) {
	body := strings.Replace(composeFixtureBody(t), `"version": 2`, `"version": 1`, 1)
	setCurrentContract(t, writeContract(t, body))
	t.Setenv("RC_LIVE_GITLAB_OWNER", "")

	recorder := &fatalRecorder{}
	if Selected(recorder, provider.ForgeGitLab) {
		t.Fatal("malformed v1 contract was selected")
	}

	if !recorder.fatal || recorder.skipped {
		t.Fatalf("malformed contract became a skip: %+v", recorder)
	}
}

func TestTokenMeta_ProjectsV2Lifecycle(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, composeFixtureBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	forgejo, _ := contract.endpointFor("forgejo", "")

	meta := forgejo.tokenMeta(contract.Generation.ID)
	if !meta.revocationRequired || meta.revocationRunID != contract.Generation.ID {
		t.Fatalf("forgejo token metadata = %+v", meta)
	}

	gitlab, _ := contract.endpointFor("gitlab", "")

	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	if err := validateTokenMetadata("gitlab", contract.Generation.ID, gitlab.tokenMeta(contract.Generation.ID), now); err != nil {
		t.Fatalf("canonical GitLab token lifecycle rejected: %v", err)
	}
}

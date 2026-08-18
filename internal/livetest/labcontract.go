// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	contractVersion     = 2
	maxContractBytes    = 64 * 1024
	endpointKindForgejo = "forgejo"
	endpointKindGitea   = "gitea"
)

type requiredNullable[T any] struct {
	Set   bool
	Value *T
}

func (value *requiredNullable[T]) UnmarshalJSON(body []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		value.Value = nil

		return nil
	}

	var decoded T

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&decoded); err != nil {
		return err
	}

	if err := requireJSONEOF(decoder); err != nil {
		return err
	}

	value.Value = &decoded

	return nil
}

func (value requiredNullable[T]) MarshalJSON() ([]byte, error) {
	if value.Value == nil {
		return []byte("null"), nil
	}

	return json.Marshal(value.Value)
}

type labContract struct {
	Version    int                         `json:"version"`
	Generation labGeneration               `json:"generation"`
	CAFile     requiredNullable[string]    `json:"ca_file"`
	Endpoints  []labEndpoint               `json:"endpoints"`
	Fulcio     requiredNullable[labFulcio] `json:"fulcio"`
	Interfaces *labInterfaces              `json:"interfaces"`
}

type labGeneration struct {
	ID string `json:"id"`
}

type labInterfaces struct {
	CredentialCleanup       *labCommandInterface `json:"credential_cleanup"`
	ExtendedFixtureProducer *labFixtureInterface `json:"extended_fixture_producer"`
}

type labCommandInterface struct {
	Command      string `json:"command"`
	ContractFile string `json:"contract_file"`
}

type labFixtureInterface struct {
	Command  string `json:"command"`
	Protocol string `json:"protocol"`
}

type labEndpoint struct {
	Name         string                           `json:"name"`
	Kind         string                           `json:"kind"`
	WebBaseURL   string                           `json:"web_base_url"`
	APIBaseURL   string                           `json:"api_base_url"`
	GitBaseURL   *string                          `json:"git_base_url"`
	Capabilities []string                         `json:"capabilities"`
	OCIRegistry  requiredNullable[labOCIRegistry] `json:"oci_registry"`
	Credential   labCredential                    `json:"credential"`
}

type labOCIRegistry struct {
	BaseURL    string `json:"base_url"`
	Credential string `json:"credential"`
}

type labCredential struct {
	Username   string         `json:"username"`
	Token      string         `json:"token"`
	ProviderID *string        `json:"provider_id"`
	Name       string         `json:"name"`
	CreatedAt  string         `json:"created_at"`
	ExpiresAt  *string        `json:"expires_at"`
	Revocation *labRevocation `json:"revocation"`
}

type labRevocation struct {
	Required     bool    `json:"required"`
	GenerationID *string `json:"generation_id"`
}

type labFulcio struct {
	BaseURL string            `json:"base_url"`
	Issuers []labFulcioIssuer `json:"issuers"`
}

type labFulcioIssuer struct {
	Endpoint   string `json:"endpoint"`
	OIDCIssuer string `json:"oidc_issuer"`
}

type parsedHTTPSURL struct {
	host     string
	hostname string
	port     string
	path     string
}

var (
	generationIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,39}$`)
	endpointNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	httpsURLPattern     = regexp.MustCompile(`^https://([A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*)(?::([0-9]{1,5}))?(/[^?#]*)?$`)
	rfc3339ValuePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]+)?(?:Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$`)
	providerDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

func loadLabContract(path string) (labContract, error) {
	body, err := readPrivateContract(path)
	if err != nil {
		return labContract{}, err
	}

	return decodeLabContract(body)
}

func readPrivateContract(path string) ([]byte, error) {
	body, _, err := readPrivateContractBound(path)

	return body, err
}

func decodeLabContract(body []byte) (labContract, error) {
	if !utf8.Valid(body) {
		return labContract{}, fmt.Errorf("live-target contract is not valid UTF-8: %w", errs.ErrMalformedInput)
	}

	if err := rejectDuplicateObjectKeys(body); err != nil {
		return labContract{}, fmt.Errorf("decode live-target contract: %w: %w", err, errs.ErrMalformedInput)
	}

	if err := validateExactContractShape(body); err != nil {
		return labContract{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	var contract labContract
	if err := decoder.Decode(&contract); err != nil {
		return labContract{}, fmt.Errorf("decode live-target contract: %w: %w", err, errs.ErrMalformedInput)
	}

	if err := requireJSONEOF(decoder); err != nil {
		return labContract{}, fmt.Errorf("live-target contract has trailing data: %w: %w", err, errs.ErrMalformedInput)
	}

	if err := contract.validateWire(); err != nil {
		return labContract{}, err
	}
	contract.normalizeCapabilities()

	return contract, nil
}

//nolint:cyclop,gocyclo,gocognit,goconst,nestif,govet // One explicit branch per exact wire object boundary is intentional.
func validateExactContractShape(body []byte) error {
	root, err := exactObject(body, "contract")
	if err != nil {
		return err
	}

	if err := exactKeys(root, "contract", []string{"version"},
		[]string{"generation", "ca_file", "endpoints", "fulcio", "interfaces"}); err != nil {
		return err
	}

	var version int
	if err := json.Unmarshal(root["version"], &version); err != nil {
		return fmt.Errorf("contract.version must be an integer: %w", errs.ErrMalformedInput)
	}

	if version != contractVersion {
		return contractVersionError(version)
	}

	if err := exactKeys(root, "contract",
		[]string{"version", "generation", "ca_file", "endpoints", "fulcio"}, []string{"interfaces"}); err != nil {
		return err
	}

	generation, err := exactObject(root["generation"], "generation")
	if err != nil {
		return err
	}

	if err := exactKeys(generation, "generation", []string{"id"}, nil); err != nil {
		return err
	}

	var endpoints []json.RawMessage
	if bytes.Equal(bytes.TrimSpace(root["endpoints"]), []byte("null")) || json.Unmarshal(root["endpoints"], &endpoints) != nil {
		return fmt.Errorf("contract.endpoints must be an array: %w", errs.ErrMalformedInput)
	}

	for index, raw := range endpoints {
		label := fmt.Sprintf("endpoints[%d]", index)

		endpoint, endpointErr := exactObject(raw, label)
		if endpointErr != nil {
			return endpointErr
		}

		if endpointErr = exactKeys(endpoint, label,
			[]string{"name", "kind", "web_base_url", "api_base_url", "capabilities", "oci_registry", "credential"},
			[]string{"git_base_url"}); endpointErr != nil {
			return endpointErr
		}

		if registryRaw := endpoint["oci_registry"]; !bytes.Equal(bytes.TrimSpace(registryRaw), []byte("null")) {
			registry, registryErr := exactObject(registryRaw, label+".oci_registry")
			if registryErr != nil {
				return registryErr
			}

			if registryErr = exactKeys(registry, label+".oci_registry", []string{"base_url", "credential"}, nil); registryErr != nil {
				return registryErr
			}
		}

		credential, credentialErr := exactObject(endpoint["credential"], label+".credential")
		if credentialErr != nil {
			return credentialErr
		}

		if credentialErr = exactKeys(credential, label+".credential",
			[]string{"username", "token", "name", "created_at", "revocation"},
			[]string{"provider_id", "expires_at"}); credentialErr != nil {
			return credentialErr
		}

		revocation, revocationErr := exactObject(credential["revocation"], label+".credential.revocation")
		if revocationErr != nil {
			return revocationErr
		}

		if revocationErr = exactKeys(revocation, label+".credential.revocation",
			[]string{"required"}, []string{"generation_id"}); revocationErr != nil {
			return revocationErr
		}

		var required bool
		if bytes.Equal(bytes.TrimSpace(revocation["required"]), []byte("null")) ||
			json.Unmarshal(revocation["required"], &required) != nil {
			return fmt.Errorf("%s.credential.revocation.required must be a boolean: %w", label, errs.ErrMalformedInput)
		}
	}

	if fulcioRaw := root["fulcio"]; !bytes.Equal(bytes.TrimSpace(fulcioRaw), []byte("null")) {
		fulcio, fulcioErr := exactObject(fulcioRaw, "fulcio")
		if fulcioErr != nil {
			return fulcioErr
		}

		if fulcioErr = exactKeys(fulcio, "fulcio", []string{"base_url", "issuers"}, nil); fulcioErr != nil {
			return fulcioErr
		}

		var issuers []json.RawMessage
		if bytes.Equal(bytes.TrimSpace(fulcio["issuers"]), []byte("null")) || json.Unmarshal(fulcio["issuers"], &issuers) != nil {
			return fmt.Errorf("fulcio.issuers must be an array: %w", errs.ErrMalformedInput)
		}

		for index, raw := range issuers {
			label := fmt.Sprintf("fulcio.issuers[%d]", index)

			issuer, issuerErr := exactObject(raw, label)
			if issuerErr != nil {
				return issuerErr
			}

			if issuerErr = exactKeys(issuer, label, []string{"endpoint", "oidc_issuer"}, nil); issuerErr != nil {
				return issuerErr
			}
		}
	}

	interfacesRaw, hasInterfaces := root["interfaces"]
	if !hasInterfaces || bytes.Equal(bytes.TrimSpace(interfacesRaw), []byte("null")) {
		return nil
	}

	interfaces, err := exactObject(interfacesRaw, "interfaces")
	if err != nil {
		return err
	}

	if err := exactKeys(interfaces, "interfaces", nil,
		[]string{"credential_cleanup", "extended_fixture_producer"}); err != nil {
		return err
	}

	if cleanupRaw, ok := interfaces["credential_cleanup"]; ok && !bytes.Equal(bytes.TrimSpace(cleanupRaw), []byte("null")) {
		cleanup, cleanupErr := exactObject(cleanupRaw, "interfaces.credential_cleanup")
		if cleanupErr != nil {
			return cleanupErr
		}

		if cleanupErr = exactKeys(cleanup, "interfaces.credential_cleanup", []string{"command", "contract_file"}, nil); cleanupErr != nil {
			return cleanupErr
		}
	}

	if fixtureRaw, ok := interfaces["extended_fixture_producer"]; ok && !bytes.Equal(bytes.TrimSpace(fixtureRaw), []byte("null")) {
		fixture, fixtureErr := exactObject(fixtureRaw, "interfaces.extended_fixture_producer")
		if fixtureErr != nil {
			return fixtureErr
		}

		if fixtureErr = exactKeys(fixture, "interfaces.extended_fixture_producer", []string{"command", "protocol"}, nil); fixtureErr != nil {
			return fixtureErr
		}
	}

	return nil
}

func exactObject(body []byte, label string) (map[string]json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return nil, fmt.Errorf("%s must be an object, not null: %w", label, errs.ErrMalformedInput)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be an object: %w", label, errs.ErrMalformedInput)
	}

	return object, nil
}

func exactKeys(object map[string]json.RawMessage, label string, required, optional []string) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, ok := object[key]; !ok {
			return fmt.Errorf("%s is missing required key %q: %w", label, key, errs.ErrMalformedInput)
		}
	}

	for _, key := range optional {
		allowed[key] = struct{}{}
	}

	for key := range object {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%s contains unknown or case-mismatched key %q: %w", label, key, errs.ErrMalformedInput)
		}
	}

	return nil
}

//nolint:cyclop,gosec // Validation deliberately checks every filesystem invariant.
func readPrivateContractBound(path string) ([]byte, string, error) { //nolint:gocyclo // Ordered file identity checks are intentionally explicit.
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, "", fmt.Errorf("contract path %q must be a clean absolute path: %w", path, errs.ErrValidation)
	}

	parent := filepath.Dir(path)

	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil || resolvedParent != parent {
		return nil, "", fmt.Errorf("contract parent %q must not contain symbolic links: %w", parent, errs.ErrValidation)
	}

	before, err := os.Lstat(path) //nolint:gosec // Clean absolute operator input is the file being validated.
	if err != nil {
		return nil, "", fmt.Errorf("inspect live-target contract: %w", err)
	}

	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("live-target contract must be a regular non-symlink file: %w", errs.ErrValidation)
	}

	if before.Mode().Perm() != 0o400 && before.Mode().Perm() != 0o600 {
		return nil, "", fmt.Errorf("live-target contract must have mode 0400 or 0600: %w", errs.ErrValidation)
	}

	if stat, ok := before.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return nil, "", fmt.Errorf("live-target contract must be owned by the current user: %w", errs.ErrValidation)
	}

	if before.Size() > maxContractBytes {
		return nil, "", fmt.Errorf("live-target contract exceeds %d bytes: %w", maxContractBytes, errs.ErrValidation)
	}

	file, err := os.Open(path) //nolint:gosec // operator-selected contract is the input being validated.
	if err != nil {
		return nil, "", fmt.Errorf("read live-target contract: %w", err)
	}
	defer func() { _ = file.Close() }()

	opened, err := file.Stat()

	var openedStat *syscall.Stat_t

	openedStatOK := false
	if err == nil {
		openedStat, openedStatOK = opened.Sys().(*syscall.Stat_t)
	}

	if err != nil || !os.SameFile(before, opened) || before.Mode() != opened.Mode() ||
		!openedStatOK || openedStat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return nil, "", fmt.Errorf("live-target contract changed while it was opened: %w", errs.ErrValidation)
	}

	body, err := io.ReadAll(io.LimitReader(file, maxContractBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read live-target contract: %w", err)
	}

	if len(body) > maxContractBytes {
		return nil, "", fmt.Errorf("live-target contract exceeds %d bytes: %w", maxContractBytes, errs.ErrValidation)
	}

	after, err := file.Stat()

	var afterStat *syscall.Stat_t

	afterStatOK := false
	if err == nil {
		afterStat, afterStatOK = after.Sys().(*syscall.Stat_t)
	}

	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() ||
		opened.Mode() != after.Mode() || opened.ModTime() != after.ModTime() ||
		!afterStatOK || afterStat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Effective UIDs are non-negative on supported Unix runners.
		return nil, "", fmt.Errorf("live-target contract changed while it was read: %w", errs.ErrValidation)
	}

	pathAfter, err := os.Lstat(path) //nolint:gosec // Recheck the accepted pathname after reading its descriptor.
	if err != nil || !os.SameFile(before, pathAfter) {
		return nil, "", fmt.Errorf("live-target contract path was replaced while it was read: %w", errs.ErrValidation)
	}

	facts, err := fileBindingFacts(before, body)
	if err != nil {
		return nil, "", fmt.Errorf("bind live-target contract facts: %w", err)
	}

	return body, facts, nil
}

func rejectDuplicateObjectKeys(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}

	return requireJSONEOF(decoder)
}

func inspectJSONValue(decoder *json.Decoder) error { //nolint:cyclop // Recursive JSON token grammar.
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := map[string]struct{}{}

		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}

			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string: %w", errs.ErrMalformedInput)
			}

			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object key %q: %w", key, errs.ErrMalformedInput)
			}

			seen[key] = struct{}{}

			if valueErr := inspectJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
	case '[':
		for decoder.More() {
			if valueErr := inspectJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q: %w", delimiter, errs.ErrMalformedInput)
	}

	_, err = decoder.Token()

	return err
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("another JSON value follows the contract: %w", errs.ErrMalformedInput)
		}

		return err
	}

	return nil
}

func (contract labContract) validateWire() error { //nolint:cyclop // Complete contract validation is intentionally centralized.
	if contract.Version != contractVersion {
		return contractVersionError(contract.Version)
	}

	if !generationIDPattern.MatchString(contract.Generation.ID) {
		return fmt.Errorf("generation.id %q is invalid: %w", contract.Generation.ID, errs.ErrValidation)
	}

	if !contract.CAFile.Set {
		return fmt.Errorf("ca_file is required and may be null: %w", errs.ErrValidation)
	}

	if contract.CAFile.Value != nil {
		if err := validateAbsoluteDataPath("ca_file", *contract.CAFile.Value); err != nil {
			return err
		}
	}

	if len(contract.Endpoints) == 0 {
		return fmt.Errorf("endpoints must contain at least one endpoint: %w", errs.ErrValidation)
	}

	if !contract.Fulcio.Set {
		return fmt.Errorf("fulcio is required and may be null: %w", errs.ErrValidation)
	}

	names := make(map[string]struct{}, len(contract.Endpoints))

	for i := range contract.Endpoints {
		endpoint := &contract.Endpoints[i]
		if _, exists := names[endpoint.Name]; exists {
			return fmt.Errorf("endpoint name %q is duplicated: %w", endpoint.Name, errs.ErrValidation)
		}

		names[endpoint.Name] = struct{}{}
		if err := endpoint.validateWire(contract.Generation.ID); err != nil {
			return err
		}
	}

	if contract.Fulcio.Value != nil {
		if err := contract.Fulcio.Value.validateWire(names); err != nil {
			return err
		}
	}

	if err := validateLocalContractTopology(contract); err != nil {
		return err
	}

	return contract.validateInterfaces()
}

func contractVersionError(version int) error {
	if version == 1 {
		return fmt.Errorf("live-target contract version 1 is retired; version 2 is required: %w", errs.ErrValidation)
	}

	return fmt.Errorf("live-target contract version must be 2, got %d: %w", version, errs.ErrValidation)
}

func (endpoint labEndpoint) validateWire(generationID string) error { //nolint:cyclop,gocognit,goconst // One branch per endpoint invariant.
	if !endpointNamePattern.MatchString(endpoint.Name) || strings.HasSuffix(endpoint.Name, "-") {
		return fmt.Errorf("endpoint name %q is invalid: %w", endpoint.Name, errs.ErrValidation)
	}

	switch endpoint.Kind {
	case "github", "gitlab", endpointKindForgejo, endpointKindGitea:
	default:
		return fmt.Errorf("endpoint %q has unsupported kind %q: %w", endpoint.Name, endpoint.Kind, errs.ErrValidation)
	}

	for label, value := range map[string]string{
		"web_base_url": endpoint.WebBaseURL,
		"api_base_url": endpoint.APIBaseURL,
	} {
		if _, err := parseHTTPSURL(endpoint.Name+" "+label, value, false); err != nil {
			return err
		}
	}

	if endpoint.GitBaseURL != nil {
		if _, err := parseHTTPSURL(endpoint.Name+" git_base_url", *endpoint.GitBaseURL, false); err != nil {
			return err
		}
	}

	if endpoint.Capabilities == nil {
		return fmt.Errorf("endpoint %q capabilities is required: %w", endpoint.Name, errs.ErrValidation)
	}

	for _, capability := range endpoint.Capabilities {
		if capability == "" {
			return fmt.Errorf("endpoint %q has an empty capability: %w", endpoint.Name, errs.ErrValidation)
		}
	}

	if !endpoint.OCIRegistry.Set {
		return fmt.Errorf("endpoint %q oci_registry is required and may be null: %w", endpoint.Name, errs.ErrValidation)
	}

	if endpoint.OCIRegistry.Value != nil {
		if _, err := parseHTTPSURL(endpoint.Name+" oci_registry.base_url", endpoint.OCIRegistry.Value.BaseURL, true); err != nil {
			return err
		}

		if endpoint.OCIRegistry.Value.Credential != "endpoint" {
			return fmt.Errorf("endpoint %q oci_registry.credential must equal %q: %w", endpoint.Name, "endpoint", errs.ErrValidation)
		}
	}

	credential := endpoint.Credential
	if credential.Username == "" || credential.Token == "" || credential.Name == "" {
		return fmt.Errorf("endpoint %q credential is incomplete: %w", endpoint.Name, errs.ErrValidation)
	}

	if credential.Revocation == nil {
		return fmt.Errorf("endpoint %q credential.revocation is required: %w", endpoint.Name, errs.ErrValidation)
	}

	if credential.ProviderID != nil {
		if matched, _ := regexp.MatchString(`^[1-9][0-9]*$`, *credential.ProviderID); !matched {
			return fmt.Errorf("endpoint %q credential.provider_id is invalid: %w", endpoint.Name, errs.ErrValidation)
		}
	}

	if err := validateTimestamp("credential.created_at", credential.CreatedAt); err != nil {
		return fmt.Errorf("endpoint %q: %w", endpoint.Name, err)
	}

	if credential.ExpiresAt != nil {
		if err := validateExpiryValue(*credential.ExpiresAt); err != nil {
			return fmt.Errorf("endpoint %q: %w", endpoint.Name, err)
		}

		if credential.Revocation.Required || credential.Revocation.GenerationID != nil {
			return fmt.Errorf("endpoint %q expiring credential has revocation fallback metadata: %w", endpoint.Name, errs.ErrValidation)
		}
	} else if !credential.Revocation.Required || credential.Revocation.GenerationID == nil ||
		*credential.Revocation.GenerationID != generationID {
		return fmt.Errorf("endpoint %q non-expiring credential is not bound to generation %q: %w", endpoint.Name, generationID, errs.ErrValidation)
	}

	return nil
}

func (contract *labContract) normalizeCapabilities() {
	for index := range contract.Endpoints {
		capabilities := contract.Endpoints[index].Capabilities
		unique := make([]string, 0, len(capabilities))
		seen := make(map[string]struct{}, len(capabilities))

		for _, capability := range capabilities {
			if _, duplicate := seen[capability]; duplicate {
				continue
			}

			seen[capability] = struct{}{}
			unique = append(unique, capability)
		}

		contract.Endpoints[index].Capabilities = unique
	}
}

func (fulcio labFulcio) validateWire(endpointNames map[string]struct{}) error {
	if _, err := parseHTTPSURL("fulcio.base_url", fulcio.BaseURL, true); err != nil {
		return err
	}

	if fulcio.Issuers == nil {
		return fmt.Errorf("fulcio.issuers is required and may be empty: %w", errs.ErrValidation)
	}

	seen := make(map[string]struct{}, len(fulcio.Issuers))
	for _, issuer := range fulcio.Issuers {
		if !endpointNamePattern.MatchString(issuer.Endpoint) || strings.HasSuffix(issuer.Endpoint, "-") {
			return fmt.Errorf("fulcio issuer endpoint %q is invalid: %w", issuer.Endpoint, errs.ErrValidation)
		}

		if _, exists := endpointNames[issuer.Endpoint]; !exists {
			return fmt.Errorf("fulcio issuer references unknown endpoint %q: %w", issuer.Endpoint, errs.ErrValidation)
		}

		if _, duplicate := seen[issuer.Endpoint]; duplicate {
			return fmt.Errorf("fulcio issuer endpoint %q is duplicated: %w", issuer.Endpoint, errs.ErrValidation)
		}

		seen[issuer.Endpoint] = struct{}{}
		if _, err := parseHTTPSURL("fulcio issuer "+issuer.Endpoint, issuer.OIDCIssuer, false); err != nil {
			return err
		}
	}

	return nil
}

func (contract labContract) validateInterfaces() error {
	if contract.Interfaces == nil {
		return nil
	}

	if cleanup := contract.Interfaces.CredentialCleanup; cleanup != nil {
		if err := validateAbsoluteDataPath("credential_cleanup.command", cleanup.Command); err != nil {
			return err
		}

		if err := validateAbsoluteDataPath("credential_cleanup.contract_file", cleanup.ContractFile); err != nil {
			return err
		}
	}

	if fixture := contract.Interfaces.ExtendedFixtureProducer; fixture != nil {
		if err := validateAbsoluteDataPath("extended_fixture_producer.command", fixture.Command); err != nil {
			return err
		}

		if fixture.Protocol == "" {
			return fmt.Errorf("extended_fixture_producer.protocol is empty: %w", errs.ErrValidation)
		}
	}

	return nil
}

func validateAbsoluteDataPath(label, value string) error {
	if !filepath.IsAbs(value) || value == "/" || filepath.Clean(value) != value || containsUnsafeText(value) {
		return fmt.Errorf("%s must be a clean absolute path: %w", label, errs.ErrValidation)
	}

	return nil
}

func parseHTTPSURL(label, value string, originOnly bool) (parsedHTTPSURL, error) { //nolint:cyclop // URL grammar is validated field by field.
	if containsUnsafeText(value) {
		return parsedHTTPSURL{}, fmt.Errorf("%s contains whitespace, controls, or a backslash: %w", label, errs.ErrValidation)
	}

	match := httpsURLPattern.FindStringSubmatch(value)
	if match == nil {
		return parsedHTTPSURL{}, fmt.Errorf("%s must be an HTTPS URL without userinfo, query, or fragment: %w", label, errs.ErrValidation)
	}

	if match[2] != "" {
		port, err := strconv.Atoi(match[2])
		if err != nil || port < 1 || port > 65535 {
			return parsedHTTPSURL{}, fmt.Errorf("%s has an unsafe port: %w", label, errs.ErrValidation)
		}
	}

	if originOnly && match[3] != "" && match[3] != "/" {
		return parsedHTTPSURL{}, fmt.Errorf("%s must be an HTTPS origin without a path: %w", label, errs.ErrValidation)
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return parsedHTTPSURL{}, fmt.Errorf("%s is not a safe HTTPS URL: %w", label, errs.ErrValidation)
	}

	return parsedHTTPSURL{host: parsed.Host, hostname: parsed.Hostname(), port: parsed.Port(), path: parsed.EscapedPath()}, nil
}

func containsUnsafeText(value string) bool {
	for _, character := range value {
		if character == '\\' || unicode.IsSpace(character) || character < 32 || character == 127 ||
			(character >= 128 && character < 160) {
			return true
		}
	}

	return false
}

func validateTimestamp(label, value string) error {
	if !rfc3339ValuePattern.MatchString(value) {
		return fmt.Errorf("%s must be RFC3339: %w", label, errs.ErrValidation)
	}

	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s is not a calendar-valid RFC3339 timestamp: %w", label, errs.ErrValidation)
	}

	return nil
}

func validateExpiryValue(value string) error {
	if rfc3339ValuePattern.MatchString(value) {
		return validateTimestamp("credential.expires_at", value)
	}

	if !providerDatePattern.MatchString(value) {
		return fmt.Errorf("credential.expires_at must be RFC3339 or YYYY-MM-DD: %w", errs.ErrValidation)
	}

	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return fmt.Errorf("credential.expires_at is not a calendar-valid date: %w", errs.ErrValidation)
	}

	return nil
}

func (contract labContract) endpointFor(kind, name string) (labEndpoint, error) {
	var found []labEndpoint

	for _, endpoint := range contract.Endpoints {
		if endpoint.Kind == kind && (name == "" || endpoint.Name == name) {
			found = append(found, endpoint)
		}
	}

	switch len(found) {
	case 0:
		if name != "" {
			return labEndpoint{}, fmt.Errorf("contract selects no %s endpoint named %q: %w", kind, name, errs.ErrValidation)
		}

		return labEndpoint{}, fmt.Errorf("contract selects no %s endpoint: %w", kind, errs.ErrValidation)
	case 1:
		return found[0], nil
	default:
		return labEndpoint{}, fmt.Errorf("contract carries %d %s endpoints; set RC_LIVE_%s_ENDPOINT to one exact endpoint name: %w",
			len(found), kind, strings.ToUpper(kind), errs.ErrValidation)
	}
}

func (endpoint labEndpoint) host() (string, error) {
	parsed, err := parseHTTPSURL(endpoint.Kind+" api_base_url", endpoint.APIBaseURL, false)
	if err != nil {
		return "", err
	}

	return parsed.host, nil
}

func (endpoint labEndpoint) gitURL() string {
	if endpoint.GitBaseURL == nil {
		return endpoint.WebBaseURL
	}

	return *endpoint.GitBaseURL
}

func (endpoint labEndpoint) registryOrigin() string {
	if endpoint.OCIRegistry.Value == nil {
		return ""
	}

	return strings.TrimSuffix(endpoint.OCIRegistry.Value.BaseURL, "/")
}

func (endpoint labEndpoint) forgeAuthorities() ([]string, error) {
	return contractAuthorities(endpoint.Name+" forge credential authority", endpoint.WebBaseURL, endpoint.APIBaseURL, endpoint.gitURL())
}

func (endpoint labEndpoint) registryAuthorities() ([]string, error) {
	if endpoint.OCIRegistry.Value == nil {
		return nil, nil
	}

	// The endpoint API origin is an explicit contract fact and is the token
	// service root used by GitLab; Forgejo deduplicates to its registry origin.
	return contractAuthorities(endpoint.Name+" registry credential authority", endpoint.OCIRegistry.Value.BaseURL, endpoint.APIBaseURL)
}

func oidcAuthorities(fulcioURL string) ([]string, error) {
	if fulcioURL == "" {
		return nil, nil
	}

	return contractAuthorities("Fulcio OIDC credential authority", fulcioURL)
}

func contractAuthorities(label string, values ...string) ([]string, error) {
	authorities := make([]string, 0, len(values))
	seen := map[string]struct{}{}

	for _, value := range values {
		parsed, err := parseHTTPSURL(label, value, false)
		if err != nil {
			return nil, err
		}

		authority := "https://" + parsed.host
		if _, ok := seen[authority]; ok {
			continue
		}

		seen[authority] = struct{}{}
		authorities = append(authorities, authority)
	}

	return authorities, nil
}

func (endpoint labEndpoint) requireLiveCapabilities() error {
	available := make(map[string]struct{}, len(endpoint.Capabilities))
	for _, capability := range endpoint.Capabilities {
		available[capability] = struct{}{}
	}

	for _, required := range []string{"repositories", "packages", "artifacts"} {
		if _, ok := available[required]; !ok {
			return fmt.Errorf("selected endpoint %q lacks required live capability %q: %w", endpoint.Name, required, errs.ErrValidation)
		}
	}

	if endpoint.OCIRegistry.Value == nil {
		return fmt.Errorf("selected endpoint %q declares no OCI registry for the complete live harness: %w", endpoint.Name, errs.ErrValidation)
	}

	return nil
}

func (endpoint labEndpoint) hasCapability(required string) bool {
	for _, capability := range endpoint.Capabilities {
		if capability == required {
			return true
		}
	}

	return false
}

func (contract labContract) fulcioFor(endpointName string) (string, string, bool) {
	if contract.Fulcio.Value == nil {
		return "", "", false
	}

	for _, issuer := range contract.Fulcio.Value.Issuers {
		if issuer.Endpoint == endpointName {
			return strings.TrimSuffix(contract.Fulcio.Value.BaseURL, "/"), issuer.OIDCIssuer, true
		}
	}

	return "", "", false
}

func (endpoint labEndpoint) tokenMeta(generationID string) tokenMetadata {
	meta := tokenMetadata{name: endpoint.Credential.Name, createdAt: endpoint.Credential.CreatedAt}
	if endpoint.Credential.ProviderID != nil {
		meta.id = *endpoint.Credential.ProviderID
	}

	if endpoint.Credential.ExpiresAt != nil {
		meta.expiresAt = *endpoint.Credential.ExpiresAt
	}

	if endpoint.Credential.Revocation == nil || !endpoint.Credential.Revocation.Required {
		return meta
	}

	meta.revocationRequired = true
	if endpoint.Credential.Revocation.GenerationID != nil && *endpoint.Credential.Revocation.GenerationID == generationID {
		meta.revocationRunID = generationID
	}

	return meta
}

func (contract labContract) cleanupPair() (string, string) {
	if contract.Interfaces == nil || contract.Interfaces.CredentialCleanup == nil {
		return "", ""
	}

	cleanup := contract.Interfaces.CredentialCleanup

	return cleanup.Command, cleanup.ContractFile
}

type contractCacheEntry struct {
	digest   [sha256.Size]byte
	contract labContract
}

var activeContracts = struct { //nolint:gochecknoglobals // Process cache detects mutation after the first accepted contract read.
	sync.Mutex
	byPath map[string]contractCacheEntry
}{byPath: map[string]contractCacheEntry{}}

func currentLabContract() (labContract, error) {
	path := os.Getenv(contractFileEnv)

	body, facts, err := readPrivateContractBound(path)
	if err != nil {
		return labContract{}, err
	}

	expectedFacts := os.Getenv(frozenContractFactsEnv)
	if expectedFacts == "" || facts != expectedFacts {
		return labContract{}, fmt.Errorf("frozen live-target contract does not match preflight facts: %w", errs.ErrValidation)
	}

	digest := sha256.Sum256(body)

	activeContracts.Lock()
	defer activeContracts.Unlock()

	if cached, ok := activeContracts.byPath[path]; ok {
		if cached.digest != digest {
			return labContract{}, fmt.Errorf("live-target contract changed after first load: %w", errs.ErrValidation)
		}

		return cached.contract, nil
	}

	contract, err := decodeLabContract(body)
	if err != nil {
		return labContract{}, err
	}

	activeContracts.byPath[path] = contractCacheEntry{digest: digest, contract: contract}

	return contract, nil
}

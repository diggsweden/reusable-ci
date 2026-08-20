// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// GradleTarget is a publish destination reachable through the Gradle
// toolchain. Both plain-gradle and gradle-android library artifacts
// publish through these, which is why the type is named by toolchain
// rather than by project type.
//
// The values are deliberately the same strings as the corresponding
// config.PublishTarget constants, so a config target crosses into this
// package through ParseGradleTarget without a hand-written switch.
type GradleTarget string

const (
	GradleTargetGitHubPackages GradleTarget = "github-packages"
	GradleTargetMavenCentral   GradleTarget = "maven-central"
)

// gradleShortKeyIDLen is the length of the short key id Gradle's signing
// plugin matches on.
const gradleShortKeyIDLen = 8

// gradleTargetSpec is everything the toolchain needs to know about one
// destination. Keeping the repository name and the credential table in a
// single record means adding a target is one map entry, not an edit to
// three parallel tables that can drift apart.
type gradleTargetSpec struct {
	// repositoryName is the Gradle `repositories { maven { name = … } }`
	// name the target expects the adopter's build script to declare. It
	// is the half of the publish-task convention the doctor validates.
	repositoryName string
	// bindings maps Gradle project properties to the credential slots
	// that fill them.
	//
	// The signing trio is bound under *two* spellings on purpose. The
	// plain `maven-publish` + `signing` idiom reads signingKey /
	// signingKeyId / signingPassword, while vanniktech's
	// gradle-maven-publish-plugin reads signingInMemoryKey{,Id,Password}
	// — and this CI cannot know which plugin the adopter applied.
	// Binding only one spelling means a project on the other plugin
	// silently skips signing and gets its bundle rejected by Central
	// late, with a poor message. These are environment variables on a
	// single child process; an unread one costs nothing, and the
	// duplicate carries no extra exposure because it is the same key
	// material already in the environment for the other spelling.
	bindings []CredentialBinding
}

//nolint:gochecknoglobals // immutable lookup table.
var gradleTargets = map[GradleTarget]gradleTargetSpec{
	GradleTargetGitHubPackages: {
		repositoryName: "GitHubPackages",
		bindings: []CredentialBinding{
			{Property: "githubActor", Slot: SlotGitHubActor},
			{Property: "githubToken", Slot: SlotGitHubToken},
		},
	},
	GradleTargetMavenCentral: {
		repositoryName: "MavenCentral",
		bindings: []CredentialBinding{
			{Property: "mavenCentralUsername", Slot: SlotMavenCentralUsername},
			{Property: "mavenCentralPassword", Slot: SlotMavenCentralPassword},

			// Plain maven-publish + signing spelling.
			{Property: "signingKeyId", Slot: SlotSigningKeyID},
			{Property: "signingKey", Slot: SlotSigningKey},
			{Property: "signingPassword", Slot: SlotSigningPassphrase},

			// vanniktech gradle-maven-publish-plugin spelling (D7).
			{Property: "signingInMemoryKeyId", Slot: SlotSigningKeyID},
			{Property: "signingInMemoryKey", Slot: SlotSigningKey},
			{Property: "signingInMemoryKeyPassword", Slot: SlotSigningPassphrase},
		},
	},
}

// ValidGradleTargets is the ordered set, for help text and validation.
// Derived from gradleTargets so it cannot fall out of step with it.
//
//nolint:gochecknoglobals // immutable lookup table.
var ValidGradleTargets = slices.Sorted(maps.Keys(gradleTargets))

// GradleRepositoryName returns the expected repository name for a target,
// or "" for an unknown target.
func GradleRepositoryName(target GradleTarget) string {
	return gradleTargets[target].repositoryName
}

// unknownGradleTargetError is the single phrasing for an unrecognised
// target, shared by every entry point that validates one.
func unknownGradleTargetError(raw any) error {
	return fmt.Errorf("unknown gradle publish target %q (want one of %v): %w",
		raw, ValidGradleTargets, errs.ErrUsage)
}

// PublishTasks returns the Gradle task list for a target, or the
// adopter's override (whitespace-split) when non-empty. An unknown
// target is errs.ErrUsage.
//
// The derived tasks are the *named-repository* forms
// (publishAllPublicationsTo<Name>Repository), deliberately not the bare
// `publish` task: `publish` pushes every publication to *every*
// configured repository, so a project set up for both destinations would
// publish to Central from inside the GitHub-Packages job. The named
// tasks are the only per-destination ones, which is what keeps the two
// publish-stage jobs independent.
//
// The cost is that this encodes a repository-naming convention the
// adopter's build script has to follow (see GradleRepositoryName). That
// is a deliberate trade: it is a one-line requirement, publish-tasks
// overrides it, and the doctor checks report the actual names found in
// the build script — so a mismatch surfaces as a local, actionable
// message rather than a bare Gradle "task not found".
//
// Users of vanniktech's gradle-maven-publish-plugin override this with
// "publishToMavenCentral".
func PublishTasks(target GradleTarget, override string) ([]string, error) {
	if fields := strings.Fields(override); len(fields) > 0 {
		return fields, nil
	}

	spec, ok := gradleTargets[target]
	if !ok {
		return nil, unknownGradleTargetError(target)
	}

	return []string{"publishAllPublicationsTo" + spec.repositoryName + "Repository"}, nil
}

// CredentialSlot names where the app layer sources a credential value.
// The domain knows which slots a target needs and which Gradle property
// each maps to; it never sees a value.
type CredentialSlot string

const (
	// SlotGitHubActor / SlotGitHubToken come from the ambient GitHub
	// Actions environment rather than a declared secret.
	SlotGitHubActor CredentialSlot = "GITHUB_ACTOR"
	SlotGitHubToken CredentialSlot = "GITHUB_TOKEN"

	SlotMavenCentralUsername CredentialSlot = "MAVEN_CENTRAL_USERNAME"
	SlotMavenCentralPassword CredentialSlot = "MAVEN_CENTRAL_PASSWORD"

	// SlotSigningKeyID and SlotSigningKey are filled in-process from the
	// imported keyring (fingerprint/keyid and an armored re-export), not
	// read from the environment. Gradle's useInMemoryPgpKeys goes through
	// Bouncycastle, which reads only RFC 4880 packets, so the key has to
	// be re-exported rather than passed through as the raw secret.
	SlotSigningKeyID      CredentialSlot = "SIGNING_KEY_ID"
	SlotSigningKey        CredentialSlot = "SIGNING_KEY"
	SlotSigningPassphrase CredentialSlot = "RELEASE_GPG_PASSPHRASE"
)

// CredentialBinding maps one Gradle project property to the slot whose
// value fills it. Property "foo" is passed to Gradle as the environment
// variable ORG_GRADLE_PROJECT_foo. Every binding is required; an
// optional one would need a distinct resolution rule, so the day one
// appears it should arrive with its own field and a test.
type CredentialBinding struct {
	Property string
	Slot     CredentialSlot
}

// EnvVar returns the environment variable name Gradle reads this
// property from.
func (b CredentialBinding) EnvVar() string { return "ORG_GRADLE_PROJECT_" + b.Property }

// CredentialBindings returns the property→slot table for a target, or
// nil for an unknown target.
func CredentialBindings(target GradleTarget) []CredentialBinding {
	return gradleTargets[target].bindings
}

// ShortSigningKeyID reduces a GPG key id to the 8-character short form
// Gradle's signing plugin matches on. `release gpg import` emits the
// 16-character long id, which both useInMemoryPgpKeys and vanniktech's
// signingInMemoryKeyId reject with "could not find secret key".
// Shorter-or-equal input is returned unchanged.
func ShortSigningKeyID(keyID string) string {
	id := strings.TrimSpace(keyID)
	if len(id) <= gradleShortKeyIDLen {
		return id
	}

	return id[len(id)-gradleShortKeyIDLen:]
}

// ParseGradleTarget validates a raw target string. It is also how a
// config.PublishTarget crosses into this package: the constants share
// their string values, so a non-Gradle destination (google-play, npmjs)
// simply reports as unknown here.
func ParseGradleTarget(raw string) (GradleTarget, error) {
	target := GradleTarget(strings.TrimSpace(raw))
	if _, ok := gradleTargets[target]; !ok {
		return "", unknownGradleTargetError(raw)
	}

	return target, nil
}

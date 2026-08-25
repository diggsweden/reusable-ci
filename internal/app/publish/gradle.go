// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainpublish "github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

// GradleOps is the slice of the gradle adapter this use case needs.
//
// It is deliberately a different, narrower port than app/build's
// GradleOps: this path needs exactly one verb, and a shared interface
// would drag the build verbs into a publish job that must not run them.
type GradleOps interface {
	RunInDirEnvInherit(ctx context.Context, dir string, env map[string]string, stdout, stderr io.Writer, args ...string) error
}

// GradleSigningOps is the slice of the gpg adapter needed to re-export
// the signing key for Gradle. Declared separately from GradleOps because
// only the maven-central target uses it — a forge-packages deploy can
// pass nil.
type GradleSigningOps interface {
	ExportSecretKey(ctx context.Context, fingerprint, passphrase string) (string, error)
}

// GradleDeployInput drives GradleDeploy.
type GradleDeployInput struct {
	// Target is the publish destination; it selects both the derived
	// gradle task and the credential binding set.
	Target domainpublish.GradleTarget
	// Tasks overrides the target-derived publish task. Empty is the norm.
	Tasks string
	// WorkingDirectory is where ./gradlew is invoked. Empty → cwd.
	WorkingDirectory string
	// Fingerprint is the imported signing key's fingerprint, from
	// `reusable-ci release gpg import`. Required for maven-central.
	Fingerprint string
	// KeyID is that key's id, as `release gpg import` emits it. Reduced
	// to the short form Gradle matches on before it is bound.
	KeyID string
	// LookupEnv resolves an environment-sourced credential slot to its
	// value. Injected so tests don't have to mutate the process
	// environment; nil → os.Getenv. Only the Central and passphrase
	// slots go through it — the forge credentials come from the
	// resolver, which is the whole reason this layer does not name any
	// forge's variables.
	LookupEnv func(string) string
}

// GradleDeploy publishes from source with `./gradlew <tasks> --no-daemon`.
//
// Unlike the maven path, this does not consume a downloaded artifact:
// Gradle's maven-publish needs the project, so the job rebuilds. The
// build stage's uploaded JAR/AAR still matters for release attachment
// and SBOMs — it is simply not the input here.
//
// resolver supplies the forge-native registry credentials and may be nil
// for a target that needs none (maven-central). It is the same provider
// role ForgePackagesDeploy uses, so a forge gains Gradle publishing by
// implementing one interface rather than by a switch in here.
//
// Every credential is resolved and checked *before* gradle starts, so a
// missing secret fails in a second with a clear message rather than
// minutes into a build. Credential values go only into the child
// process environment — never to stdout, a sink, or a step output.
func GradleDeploy( //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	ctx context.Context,
	ops GradleOps,
	signing GradleSigningOps,
	resolver provider.ForgeMavenRegistryResolver,
	w, stderr io.Writer,
	in GradleDeployInput,
) error {
	if ops == nil {
		return fmt.Errorf("gradle ops is required: %w", errs.ErrUsage)
	}

	tasks, err := domainpublish.PublishTasks(in.Target, in.Tasks)
	if err != nil {
		return err
	}

	env, err := resolveGradleCredentials(ctx, signing, resolver, in)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Publishing to %s via Gradle...\n", in.Target)
	_, _ = fmt.Fprintf(w, "Tasks: %s\n", strings.Join(tasks, " "))

	// --no-daemon is added here rather than in the domain table: the
	// table says *what* to publish, not how to run gradle. A lingering
	// daemon would hold the credential env of a finished job.
	args := make([]string, 0, len(tasks)+1)
	args = append(args, tasks...)
	args = append(args, "--no-daemon")

	if err := ops.RunInDirEnvInherit(ctx, in.WorkingDirectory, env, w, stderr, args...); err != nil {
		return fmt.Errorf("gradle publish to %s: %w", in.Target, err)
	}

	_, _ = fmt.Fprintf(w, "%s Published to %s\n", clicolor.Check(w), in.Target)

	return nil
}

// resolveGradleCredentials turns the target's binding table into the
// child-process environment.
//
// The forge and environment credentials are all resolved and checked
// *before* the signing key is exported, so the most likely
// misconfiguration — a missing Central secret — fails in milliseconds
// without spawning a gpg process.
func resolveGradleCredentials(
	ctx context.Context,
	signing GradleSigningOps,
	resolver provider.ForgeMavenRegistryResolver,
	in GradleDeployInput,
) (map[string]string, error) {
	bindings := domainpublish.CredentialBindings(in.Target)
	if len(bindings) == 0 {
		return nil, fmt.Errorf("no credential bindings for gradle publish target %q: %w", in.Target, errs.ErrUsage)
	}

	forge, err := resolveForgeCredentials(resolver, in.Target)
	if err != nil {
		return nil, err
	}

	lookup := in.LookupEnv
	if lookup == nil {
		lookup = os.Getenv
	}

	shortKeyID := domainpublish.ShortSigningKeyID(in.KeyID)
	env := make(map[string]string, len(bindings))

	// Pass 1 — everything already in hand, resolved from the forge, or
	// readable from the environment. SlotSigningKey is deferred to pass 2.
	for _, b := range bindings { //nolint:varnamelen // idiomatic short name for a binding.
		if b.Slot == domainpublish.SlotSigningKey {
			continue
		}

		var value string

		switch {
		case b.Slot == domainpublish.SlotSigningKeyID:
			value = shortKeyID
		case b.Slot.FromForgeRegistry():
			value = forge[b.Slot]
		default:
			value = lookup(string(b.Slot))
		}

		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf(
				"missing credential %s for gradle publish target %q (gradle property %q): %w",
				b.Slot, in.Target, b.Property, errs.ErrPermissionDenied)
		}

		env[b.EnvVar()] = value
	}

	// Pass 2 — the signing key is re-exported from the keyring once, not
	// once per binding: the plain and vanniktech spellings share it.
	if !slices.ContainsFunc(bindings, func(b domainpublish.CredentialBinding) bool {
		return b.Slot == domainpublish.SlotSigningKey
	}) {
		return env, nil
	}

	key, err := exportSigningKey(ctx, signing, in, lookup)
	if err != nil {
		return nil, err
	}

	for _, b := range bindings {
		if b.Slot == domainpublish.SlotSigningKey {
			env[b.EnvVar()] = key
		}
	}

	return env, nil
}

// resolveForgeCredentials asks the provider role for the forge-native
// Maven registry and maps it onto the forge credential slots. Returns an
// empty map for a target that draws nothing from the forge, so the
// caller never has to special-case maven-central.
func resolveForgeCredentials(
	resolver provider.ForgeMavenRegistryResolver,
	target domainpublish.GradleTarget,
) (map[domainpublish.CredentialSlot]string, error) {
	if !domainpublish.NeedsForgeRegistry(target) {
		return nil, nil
	}

	if resolver == nil {
		return nil, fmt.Errorf(
			"a forge package registry is required for gradle publish target %q: %w", target, errs.ErrUsage)
	}

	reg, err := resolver.ResolveForgeMavenRegistry()
	if err != nil {
		return nil, err
	}

	return map[domainpublish.CredentialSlot]string{
		domainpublish.SlotForgeActor: reg.Username,
		domainpublish.SlotForgeToken: reg.Token,
	}, nil
}

// exportSigningKey re-exports the imported key in the RFC 4880 armored
// form Gradle's Bouncycastle-backed useInMemoryPgpKeys can read.
func exportSigningKey(ctx context.Context, signing GradleSigningOps, in GradleDeployInput, lookup func(string) string) (string, error) {
	if signing == nil {
		return "", fmt.Errorf("signing ops is required for gradle publish target %q: %w", in.Target, errs.ErrUsage)
	}

	if strings.TrimSpace(in.Fingerprint) == "" {
		return "", fmt.Errorf(
			"signing key fingerprint is required for gradle publish target %q; run `reusable-ci release gpg import` first: %w",
			in.Target, errs.ErrUsage)
	}

	key, err := signing.ExportSecretKey(ctx, in.Fingerprint, lookup(string(domainpublish.SlotSigningPassphrase)))
	if err != nil {
		return "", fmt.Errorf("export signing key for gradle: %w", err)
	}

	return key, nil
}

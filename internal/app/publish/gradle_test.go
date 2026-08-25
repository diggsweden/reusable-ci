// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainpublish "github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

type fakeGradleOps struct {
	called bool
	dir    string
	env    map[string]string
	args   []string
	err    error
}

func (f *fakeGradleOps) RunInDirEnvInherit(_ context.Context, dir string, env map[string]string, _, _ io.Writer, args ...string) error {
	f.called = true
	f.dir = dir
	f.env = env
	f.args = args

	return f.err
}

type fakeSigningOps struct {
	called      bool
	fingerprint string
	passphrase  string
	key         string
	err         error
}

func (f *fakeSigningOps) ExportSecretKey(_ context.Context, fingerprint, passphrase string) (string, error) {
	f.called = true
	f.fingerprint = fingerprint
	f.passphrase = passphrase

	return f.key, f.err
}

func envLookup(m map[string]string) func(string) string {
	return func(name string) string { return m[name] }
}

// fakeForgeResolver stands in for the provider role that supplies the
// forge-native registry credentials. It is the seam that keeps this
// layer from naming any forge's environment variables.
type fakeForgeResolver struct {
	reg    provider.ForgeMavenRegistry
	err    error
	called bool
}

func (f *fakeForgeResolver) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	f.called = true

	return f.reg, f.err
}

func forgeResolver(username, token string) *fakeForgeResolver {
	return &fakeForgeResolver{reg: provider.ForgeMavenRegistry{Username: username, Token: token}}
}

func fullCentralEnv() map[string]string {
	return map[string]string{
		"MAVEN_CENTRAL_USERNAME": "user",
		"MAVEN_CENTRAL_PASSWORD": "pass",
		"RELEASE_GPG_PASSPHRASE": "phrase",
	}
}

func TestGradleDeploy_ForgePackages(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	resolver := forgeResolver("octocat", "tok")

	err := apppublish.GradleDeploy(context.Background(), ops, nil, resolver, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:           domainpublish.GradleTargetForgePackages,
			WorkingDirectory: "libs/jvm",
			LookupEnv:        envLookup(nil),
		})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := ops.args, []string{"publishAllPublicationsToGitHubPackagesRepository", "--no-daemon"}; !equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}

	if ops.dir != "libs/jvm" {
		t.Errorf("dir = %q", ops.dir)
	}

	if got, want := ops.env["ORG_GRADLE_PROJECT_githubActor"], "octocat"; got != want {
		t.Errorf("githubActor = %q, want %q", got, want)
	}

	if got, want := ops.env["ORG_GRADLE_PROJECT_githubToken"], "tok"; got != want {
		t.Errorf("githubToken = %q, want %q", got, want)
	}
}

// The whole point of the pre-flight: a missing secret must fail before
// gradle is invoked, not minutes into a rebuild.
func TestGradleDeploy_MissingCredentialFailsBeforeGradleRuns(t *testing.T) {
	t.Parallel()

	env := fullCentralEnv()
	delete(env, "MAVEN_CENTRAL_PASSWORD")

	ops := &fakeGradleOps{}
	signing := &fakeSigningOps{key: "-----BEGIN PGP PRIVATE KEY BLOCK-----\nx\n"}

	err := apppublish.GradleDeploy(context.Background(), ops, signing, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:      domainpublish.GradleTargetMavenCentral,
			Fingerprint: "ABC123",
			KeyID:       "DEADBEEF",
			LookupEnv:   envLookup(env),
		})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want errs.ErrPermissionDenied", err)
	}

	if ops.called {
		t.Error("gradle was invoked despite a missing credential")
	}

	if !strings.Contains(err.Error(), "MAVEN_CENTRAL_PASSWORD") {
		t.Errorf("err should name the missing slot: %v", err)
	}
}

func TestGradleDeploy_MavenCentralBindsBothSigningSpellings(t *testing.T) {
	t.Parallel()

	const armored = "-----BEGIN PGP PRIVATE KEY BLOCK-----\nbody\n-----END PGP PRIVATE KEY BLOCK-----\n"

	ops := &fakeGradleOps{}
	signing := &fakeSigningOps{key: armored}

	err := apppublish.GradleDeploy(context.Background(), ops, signing, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:      domainpublish.GradleTargetMavenCentral,
			Fingerprint: "ABC123",
			KeyID:       "DEADBEEF",
			LookupEnv:   envLookup(fullCentralEnv()),
		})
	if err != nil {
		t.Fatal(err)
	}

	// The re-export must use the imported key's fingerprint and the
	// passphrase secret.
	if signing.fingerprint != "ABC123" || signing.passphrase != "phrase" {
		t.Errorf("ExportSecretKey(%q, %q)", signing.fingerprint, signing.passphrase)
	}

	// Both plugin spellings present, carrying identical values.
	pairs := [][2]string{
		{"ORG_GRADLE_PROJECT_signingKey", "ORG_GRADLE_PROJECT_signingInMemoryKey"},
		{"ORG_GRADLE_PROJECT_signingKeyId", "ORG_GRADLE_PROJECT_signingInMemoryKeyId"},
		{"ORG_GRADLE_PROJECT_signingPassword", "ORG_GRADLE_PROJECT_signingInMemoryKeyPassword"},
	}
	for _, p := range pairs {
		a, b := ops.env[p[0]], ops.env[p[1]]
		if a == "" {
			t.Errorf("%s is unset", p[0])
		}

		if a != b {
			t.Errorf("%s = %q but %s = %q; spellings must share a value", p[0], a, p[1], b)
		}
	}

	if ops.env["ORG_GRADLE_PROJECT_signingKey"] != armored {
		t.Error("signingKey is not the re-exported armored key")
	}

	if got, want := ops.env["ORG_GRADLE_PROJECT_signingKeyId"], "DEADBEEF"; got != want {
		t.Errorf("signingKeyId = %q, want %q", got, want)
	}
}

// forge-packages must not touch the keyring at all.
func TestGradleDeploy_ForgePackagesDoesNotExportKey(t *testing.T) {
	t.Parallel()

	signing := &fakeSigningOps{key: "x"}
	err := apppublish.GradleDeploy(context.Background(), &fakeGradleOps{}, signing, forgeResolver("a", "t"), io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:    domainpublish.GradleTargetForgePackages,
			LookupEnv: envLookup(nil),
		})
	if err != nil {
		t.Fatal(err)
	}

	if signing.called {
		t.Error("forge-packages deploy exported a signing key")
	}
}

func TestGradleDeploy_MissingFingerprintIsUsageError(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	err := apppublish.GradleDeploy(context.Background(), ops, &fakeSigningOps{key: "x"}, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:    domainpublish.GradleTargetMavenCentral,
			KeyID:     "DEADBEEF",
			LookupEnv: envLookup(fullCentralEnv()),
		})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want errs.ErrUsage", err)
	}

	if ops.called {
		t.Error("gradle ran without a signing key")
	}
}

func TestGradleDeploy_OverrideTasks(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	err := apppublish.GradleDeploy(context.Background(), ops, &fakeSigningOps{key: "k"}, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:      domainpublish.GradleTargetMavenCentral,
			Tasks:       "publishToMavenCentral",
			Fingerprint: "ABC",
			KeyID:       "ID",
			LookupEnv:   envLookup(fullCentralEnv()),
		})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := ops.args, []string{"publishToMavenCentral", "--no-daemon"}; !equal(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestGradleDeploy_UnknownTargetIsUsageError(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	err := apppublish.GradleDeploy(context.Background(), ops, nil, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{Target: "nexus", LookupEnv: envLookup(nil)})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want errs.ErrUsage", err)
	}

	if ops.called {
		t.Error("gradle ran for an unknown target")
	}
}

func TestGradleDeploy_ExportFailurePropagates(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	signing := &fakeSigningOps{err: errors.New("gpg exploded")}

	err := apppublish.GradleDeploy(context.Background(), ops, signing, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:      domainpublish.GradleTargetMavenCentral,
			Fingerprint: "ABC",
			KeyID:       "ID",
			LookupEnv:   envLookup(fullCentralEnv()),
		})
	if err == nil {
		t.Fatal("expected export failure to propagate")
	}

	if ops.called {
		t.Error("gradle ran after the key export failed")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// GPG_KEY_ID carries the 16-character long id, which Gradle's signing
// plugin cannot match — both spellings must receive the short form.
func TestGradleDeploy_BindsShortSigningKeyID(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	err := apppublish.GradleDeploy(context.Background(), ops, &fakeSigningOps{key: "k"}, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:      domainpublish.GradleTargetMavenCentral,
			Fingerprint: "ABC123",
			KeyID:       "A1B2C3D4E5F60718",
			LookupEnv:   envLookup(fullCentralEnv()),
		})
	if err != nil {
		t.Fatal(err)
	}

	for _, prop := range []string{"ORG_GRADLE_PROJECT_signingKeyId", "ORG_GRADLE_PROJECT_signingInMemoryKeyId"} {
		if got, want := ops.env[prop], "E5F60718"; got != want {
			t.Errorf("%s = %q, want %q", prop, got, want)
		}
	}
}

// The forge credentials come from the provider role, so a target that
// needs one and gets nil must fail before gradle starts — not fall back
// to reading some forge's variables from the environment, which is the
// coupling this seam exists to prevent.
func TestGradleDeploy_ForgePackagesRequiresResolver(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	err := apppublish.GradleDeploy(context.Background(), ops, nil, nil, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:    domainpublish.GradleTargetForgePackages,
			LookupEnv: envLookup(nil),
		})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want errs.ErrUsage", err)
	}

	if ops.called {
		t.Error("gradle ran without a forge registry")
	}
}

func TestGradleDeploy_ForgeResolverErrorPropagates(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	resolver := &fakeForgeResolver{err: errors.New("no repository configured")}

	err := apppublish.GradleDeploy(context.Background(), ops, nil, resolver, io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:    domainpublish.GradleTargetForgePackages,
			LookupEnv: envLookup(nil),
		})
	if err == nil {
		t.Fatal("expected the resolver error to propagate")
	}

	if ops.called {
		t.Error("gradle ran after the forge registry failed to resolve")
	}
}

// A forge that resolves but yields no token is the same class of failure
// as a missing declared secret, and must be caught by the same pre-flight.
func TestGradleDeploy_EmptyForgeTokenFailsPreflight(t *testing.T) {
	t.Parallel()

	ops := &fakeGradleOps{}
	err := apppublish.GradleDeploy(context.Background(), ops, nil, forgeResolver("octocat", ""), io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:    domainpublish.GradleTargetForgePackages,
			LookupEnv: envLookup(nil),
		})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want errs.ErrPermissionDenied", err)
	}

	if ops.called {
		t.Error("gradle was invoked with an empty forge token")
	}
}

// maven-central is not a forge destination: asking the provider role for
// a registry there would fail on forges that have none, for a credential
// the target never reads.
func TestGradleDeploy_MavenCentralNeverAsksTheForge(t *testing.T) {
	t.Parallel()

	resolver := &fakeForgeResolver{}
	err := apppublish.GradleDeploy(context.Background(), &fakeGradleOps{}, &fakeSigningOps{key: "k"}, resolver,
		io.Discard, io.Discard,
		apppublish.GradleDeployInput{
			Target:      domainpublish.GradleTargetMavenCentral,
			Fingerprint: "ABC",
			KeyID:       "ID",
			LookupEnv:   envLookup(fullCentralEnv()),
		})
	if err != nil {
		t.Fatal(err)
	}

	if resolver.called {
		t.Error("maven-central deploy resolved a forge registry it does not use")
	}
}

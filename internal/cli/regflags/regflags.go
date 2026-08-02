// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package regflags holds the registry-credential flag set shared by the
// container image/manifest/promotion verbs so the flag names, env vars,
// defaults, and common wording cannot drift apart. Each caller supplies
// only its verb-specific nuance: the usage wording, the env-var variant,
// the plan-file scope, and which subset of the family the verb takes.
//
// The registry password is deliberately file/env-only — it never appears
// in argv — and that rule is enforced here, in one place.
package regflags

import (
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Shared registry-credential flag names. Values are part of the CLI
// contract — docs/cli-reference.md is generated from them and a sync test
// gates any change.
const (
	FlagAuthFile     = "auth-file"
	FlagPasswordFile = "registry-password-file"
	FlagTLSVerify    = "tls-verify"
	FlagUsername     = "registry-username"
)

// Shared registry-credential env names. EnvUser is exported because the
// signer-secret hygiene list in the container package must scrub it.
const (
	// DefaultAuthFileEnv sources --auth-file unless the verb overrides it
	// (signer-image verbs) or drops it (login/logout).
	DefaultAuthFileEnv = "REUSABLE_CI_REGISTRY_AUTH_FILE"
	// EnvUser is the primary --registry-username env source.
	EnvUser = "REGISTRY_USER"

	envUserFallback = "REGISTRY_USERNAME"
	envToken        = "REGISTRY_TOKEN"
	envPassword     = "REGISTRY_PASSWORD"
)

// CredUsername and CredPassword are the errs.Credential "What" wordings
// shared with `container login`, which keeps its own flag spellings.
const (
	CredUsername = "registry username"
	CredPassword = "registry password"
)

// Shared usage wording and the shared --tls-verify default: registry TLS
// verification stays on unless explicitly disabled.
const (
	tlsVerifyDefault  = "true"
	usageTLSVerify    = "verify registry TLS certificates: true or false"
	usageUsername     = "registry username; the password is read from --registry-password-file or $REGISTRY_TOKEN / $REGISTRY_PASSWORD"
	usagePasswordFile = `file containing the registry password/token ("-" reads stdin); defaults to $REGISTRY_TOKEN then $REGISTRY_PASSWORD. The password never appears in argv.`
)

// AuthFileOpts carries the per-verb nuance of the shared --auth-file flag.
type AuthFileOpts struct {
	// Usage is the verb-specific usage wording.
	Usage string
	// Env overrides the env source (default $REUSABLE_CI_REGISTRY_AUTH_FILE);
	// the signer-image verbs pass REUSABLE_CI_SIGNER_AUTH_FILE.
	Env string
	// NoEnv drops the env source entirely: login/logout resolve the default
	// auth-file path themselves ($REGISTRY_AUTH_FILE, then $DOCKER_CONFIG,
	// then ~/.docker/config.json).
	NoEnv bool
	// PlanScope, when non-empty, additionally resolves the flag from that
	// $REUSABLE_CI_PLAN plan-file scope (flag > plan > env > default).
	PlanScope string
}

// AuthFile returns the shared --auth-file flag.
func AuthFile(opts AuthFileOpts) cli.Flag {
	if opts.NoEnv {
		return &cli.StringFlag{Name: FlagAuthFile, Usage: opts.Usage}
	}

	env := opts.Env
	if env == "" {
		env = DefaultAuthFileEnv
	}

	return &cli.StringFlag{Name: FlagAuthFile, Sources: sources(opts.PlanScope, FlagAuthFile, env), Usage: opts.Usage}
}

// TLSVerifyOpts carries the per-verb nuance of the shared --tls-verify flag.
type TLSVerifyOpts struct {
	// Env is the verb-specific env source, e.g. IMAGE_PUSH_TLS_VERIFY.
	Env string
	// PlanScope, when non-empty, additionally resolves the flag from that
	// $REUSABLE_CI_PLAN plan-file scope (flag > plan > env > default).
	PlanScope string
}

// TLSVerify returns the shared --tls-verify flag of the push verbs.
func TLSVerify(opts TLSVerifyOpts) cli.Flag {
	return &cli.StringFlag{Name: FlagTLSVerify, Value: tlsVerifyDefault, Sources: sources(opts.PlanScope, FlagTLSVerify, opts.Env), Usage: usageTLSVerify}
}

// Username returns the shared --registry-username flag of the login verbs.
func Username() cli.Flag {
	return &cli.StringFlag{Name: FlagUsername, Sources: cli.EnvVars(EnvUser, envUserFallback), Usage: usageUsername}
}

// PasswordFile returns the shared --registry-password-file flag of the
// login verbs. It deliberately has no env source: the password itself is
// resolved by LoginPassword (file > $REGISTRY_TOKEN > $REGISTRY_PASSWORD).
func PasswordFile() cli.Flag {
	return &cli.StringFlag{Name: FlagPasswordFile, Usage: usagePasswordFile}
}

// RegistryAuth is the read-back of whichever subset of the shared
// registry-credential flags the verb declared; undeclared flags read as "".
type RegistryAuth struct {
	AuthFile     string
	Username     string // whitespace-trimmed
	PasswordFile string
	TLSVerify    string
}

// Resolve reads the registry-credential flags back from the command.
func Resolve(cmd *cli.Command) RegistryAuth {
	return RegistryAuth{
		AuthFile:     cmd.String(FlagAuthFile),
		Username:     strings.TrimSpace(cmd.String(FlagUsername)),
		PasswordFile: cmd.String(FlagPasswordFile),
		TLSVerify:    cmd.String(FlagTLSVerify),
	}
}

// LoginPassword resolves the registry password for a login verb: the
// --registry-password-file content when set ("-" reads stdin), else
// $REGISTRY_TOKEN then $REGISTRY_PASSWORD — never argv. The username is
// validated first so the errs.CredentialRequired precedence stays
// username-then-password everywhere.
func LoginPassword(username, passwordFile string) (string, error) {
	if username == "" {
		return "", errs.CredentialRequired(errs.Credential{What: CredUsername, Env: EnvUser})
	}

	password, err := FileOrEnv(passwordFile, envToken, envPassword)
	if err != nil {
		return "", err
	}

	if password == "" {
		return "", errs.CredentialRequired(errs.Credential{What: CredPassword, Flag: FlagPasswordFile, Env: envToken})
	}

	return password, nil
}

// FileOrEnv resolves a secret from a file path ("-" reads stdin) when one
// is given, else from the first non-empty env var, in order. Trailing
// CR/LF bytes are trimmed. Returns "" with no error when no source has a
// value — callers decide whether the secret is required.
func FileOrEnv(filePath string, envVars ...string) (string, error) {
	if filePath != "" {
		return secret.Resolve(filePath, "")
	}

	for _, envVar := range envVars {
		if value := strings.TrimRight(os.Getenv(envVar), "\r\n"); value != "" {
			return value, nil
		}
	}

	return "", nil
}

// sources resolves a flag from the plan-file scope first when the calling
// verb is plan-scoped; an empty scope keeps the plain env chain.
func sources(planScope, key string, envNames ...string) cli.ValueSourceChain {
	if planScope == "" {
		return cli.EnvVars(envNames...)
	}

	return planfile.Vars(planScope, key, envNames...)
}

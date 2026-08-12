// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"fmt"
	"strings"
)

// Compiled string replacers reused across renders. strings.Replacer is
// immutable and safe for concurrent use, so building each one once avoids
// re-allocating its internal trie on every escape call.
//
//nolint:gochecknoglobals // read-only, concurrency-safe replacers.
var (
	xmlEscaper = strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	newlineStripper = strings.NewReplacer("\r", "", "\n", "")
)

// MavenAuthScheme is how the generated settings.xml authenticates to the
// forge-native Maven registry. The forges differ: GitHub uses a
// username/password <server>; GitLab a Job-Token httpHeader; Forgejo an
// Authorization: token httpHeader.
type MavenAuthScheme string

// Recognised MavenAuthScheme values.
const (
	MavenAuthServerPassword MavenAuthScheme = "server-password"  // GitHub Packages
	MavenAuthJobTokenHeader MavenAuthScheme = "job-token-header" // GitLab Package Registry
	MavenAuthTokenHeader    MavenAuthScheme = "token-header"     // Forgejo packages
)

// ForgeMavenRegistry is the resolved forge-native Maven deploy target — the
// coordinates and credentials to `mvn deploy` to the registry of whichever
// forge the pipeline runs on. Each provider adapter builds it from its own
// environment (no central forge switch — that's the point of the role).
type ForgeMavenRegistry struct {
	ServerID   string // settings.xml <server><id> and altDeploymentRepository id
	URL        string // deploy endpoint
	AuthScheme MavenAuthScheme
	Username   string // server-password scheme only
	Token      string // resolved from the runner's token env var
}

// ForgeMavenRegistryResolver is the provider role for forges that expose a
// native Maven package registry (GitHub Packages, GitLab Package Registry,
// Forgejo packages). Local and registry-less forges simply don't implement it,
// so deps.RequireForgeMavenRegistryResolver returns an unsupported error.
type ForgeMavenRegistryResolver interface {
	ResolveForgeMavenRegistry() (ForgeMavenRegistry, error)
}

// ForgeNPMRegistry is the resolved forge-native npm deploy target — the
// registry URL, optional scope, and token to publish to the forge's own npm
// registry. The sibling of ForgeMavenRegistry for the npm ecosystem.
type ForgeNPMRegistry struct {
	Registry string // registry URL (trailing slash where the forge requires a path-scoped token)
	Scope    string // optional npm scope (e.g. "@owner"); empty publishes registry-wide
	Token    string // resolved from the runner's token env var
}

// ForgeNPMRegistryResolver is the npm sibling of ForgeMavenRegistryResolver.
type ForgeNPMRegistryResolver interface {
	ResolveForgeNPMRegistry() (ForgeNPMRegistry, error)
}

// RenderNPMRC renders an .npmrc authenticating to this registry. The token is
// written literally (the caller writes the file 0600), so npm needs no
// NODE_AUTH_TOKEN env. The auth key is the registry URL minus scheme, matching
// npm's path-scoped _authToken convention (host-level on GitHub, path-level on
// GitLab/Forgejo).
func (r ForgeNPMRegistry) RenderNPMRC() string {
	authKey := "//" + strings.TrimSuffix(stripScheme(r.Registry), "/") + "/"

	// .npmrc is line-based: a CR/LF in the token would inject a second line.
	// Runner tokens never contain newlines, but strip them defensively — the
	// Maven path's XML-escaping has the same intent.
	token := stripNewlines(r.Token)

	var b strings.Builder //nolint:varnamelen // idiomatic short name for a strings.Builder.

	fmt.Fprintf(&b, "%s:_authToken=%s\n", authKey, token)

	if r.Scope != "" {
		fmt.Fprintf(&b, "%s:registry=%s\n", r.Scope, r.Registry)
	} else {
		fmt.Fprintf(&b, "registry=%s\n", r.Registry)
	}

	// No always-auth. npm removed it from the CLI in 2021 (7.11.1); npm 6 was
	// the last release that honoured it, and current npm reports it as an
	// unknown config and warns it will stop working outright in the next major.
	// A path-scoped _authToken is sent on every request to that path anyway, so
	// the line bought nothing and cost a warning on every publish.

	return b.String()
}

func stripScheme(url string) string {
	return strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
}

func stripNewlines(s string) string {
	return newlineStripper.Replace(s)
}

// AltDeploymentRepository is the -DaltDeploymentRepository value binding the
// deploy to this registry via the settings.xml server id.
func (r ForgeMavenRegistry) AltDeploymentRepository() string {
	return r.ServerID + "::default::" + r.URL
}

// RenderSettingsXML renders a Maven settings.xml whose single <server>
// authenticates per AuthScheme. The token lives only in this file (written
// 0600 by the caller), never on argv. Switches on AuthScheme, not forge.
func (r ForgeMavenRegistry) RenderSettingsXML() string {
	var auth string

	switch r.AuthScheme {
	case MavenAuthServerPassword:
		auth = fmt.Sprintf("      <username>%s</username>\n      <password>%s</password>",
			xmlEscape(r.Username), xmlEscape(r.Token))
	case MavenAuthJobTokenHeader:
		auth = mavenHTTPHeader("Job-Token", r.Token)
	case MavenAuthTokenHeader:
		auth = mavenHTTPHeader("Authorization", "token "+r.Token)
	}

	return fmt.Sprintf(`<settings>
  <servers>
    <server>
      <id>%s</id>
%s
    </server>
  </servers>
</settings>
`, xmlEscape(r.ServerID), auth)
}

func mavenHTTPHeader(name, value string) string {
	return fmt.Sprintf(`      <configuration>
        <httpHeaders>
          <property>
            <name>%s</name>
            <value>%s</value>
          </property>
        </httpHeaders>
      </configuration>`, xmlEscape(name), xmlEscape(value))
}

// xmlEscape escapes the five XML metacharacters so a token or name can't break
// out of the settings.xml structure.
func xmlEscape(value string) string {
	return xmlEscaper.Replace(value)
}

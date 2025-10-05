// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"net/url"
	"strings"
)

// ReleaseWorkflowScope answers whether an instance is one the pinned reusable
// release workflows can run on, and it lives here rather than in the
// application layer for the reason ADR 0004 gives: naming a concrete forge is
// forge vocabulary, and forge vocabulary belongs to the provider model.
//
// Two application call sites used to ask this by branching on
// `forge != ForgeGitHub` themselves — one in app/validate, one in app/doctor —
// which is the pattern TestAppLayerDoesNotBranchOnForgeIdentity exists to forbid and
// could not see, because it looked for `==` and `case` and not `!=`.
//
// The question is about the INSTANCE, not the forge. github.com and a GitHub
// Enterprise Server deployment are the same forge and differ here, in the same
// way Capabilities.PublicFulcioTrusted is a property of an instance rather than
// of a forge. That is why this takes a server URL alongside the forge instead
// of being a Capabilities field.
type ReleaseWorkflowScope int

const (
	// ReleaseWorkflowNotApplicable means the question does not arise: either
	// the forge does not use the pinned GitHub Actions at all, or no server URL
	// was supplied to distinguish the instance.
	ReleaseWorkflowNotApplicable ReleaseWorkflowScope = iota

	// ReleaseWorkflowSupported means the instance runs the pinned actions.
	ReleaseWorkflowSupported

	// ReleaseWorkflowUnsupportedInstance means this deployment of the forge
	// cannot: actions/upload-artifact@v7 is github.com-only, so a GitHub
	// Enterprise Server deployment fails late, after the builds have run.
	ReleaseWorkflowUnsupportedInstance

	// ReleaseWorkflowMalformedServerURL means the server URL could not be read,
	// so no claim is made either way.
	ReleaseWorkflowMalformedServerURL
)

// ClassifyReleaseWorkflowScope reports which of the above applies. Callers turn
// the result into their own diagnostic — an error in validate, a check record
// in doctor — without naming a forge.
func ClassifyReleaseWorkflowScope(forge ForgeAPI, serverURL string) ReleaseWorkflowScope {
	if forge != ForgeGitHub {
		return ReleaseWorkflowNotApplicable
	}

	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return ReleaseWorkflowNotApplicable
	}

	host, ok := instanceHost(serverURL)
	if !ok {
		return ReleaseWorkflowMalformedServerURL
	}

	if strings.EqualFold(host, "github.com") {
		return ReleaseWorkflowSupported
	}

	return ReleaseWorkflowUnsupportedInstance
}

// instanceHost extracts the hostname of a server URL, reporting false when the
// value is not a URL this can read. A malformed value is never treated as
// "some other host": that would turn a typo into a refusal with a misleading
// reason.
func instanceHost(serverURL string) (string, bool) {
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", false
	}

	return parsed.Hostname(), true
}

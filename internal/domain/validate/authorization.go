// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import "strings"

// AuthorizationOutcome describes the decision the gate produces.
type AuthorizationOutcome string

const (
	// AuthorizationSnapshotBypass — SNAPSHOT release, gate skipped.
	AuthorizationSnapshotBypass AuthorizationOutcome = "snapshot-bypass"
	// AuthorizationNoRestrictions — RELEASE_AUTHORIZED_USERS empty,
	// any user with tag-push access is allowed (with a warning).
	AuthorizationNoRestrictions AuthorizationOutcome = "no-restrictions"
	// AuthorizationAllowed — actor matched the configured CSV list.
	AuthorizationAllowed AuthorizationOutcome = "allowed"
	// AuthorizationDenied — actor not in the CSV list.
	AuthorizationDenied AuthorizationOutcome = "denied"
)

// AuthorizationDecision is the typed result of running the gate.
// AuthorizedUsers is populated only on Denied so callers can render the
// "only the following users may release" guidance.
type AuthorizationDecision struct {
	Outcome         AuthorizationOutcome
	Tag             string
	Actor           string
	AuthorizedUsers []string
}

// DecideAuthorization runs the policy from scripts/validate/authorization.sh:
//
//  1. SNAPSHOT tag → bypass (the rest of the rule doesn't apply).
//  2. authorizedDevs empty → no-restrictions (release proceeds with a
//     warning).
//  3. actor in authorizedDevs (CSV) → allowed.
//  4. otherwise → denied + the authorized list for guidance.
func DecideAuthorization(tag, actor, authorizedDevs string) AuthorizationDecision {
	d := AuthorizationDecision{Tag: tag, Actor: actor}
	if IsSnapshot(tag) {
		d.Outcome = AuthorizationSnapshotBypass
		return d
	}
	if strings.TrimSpace(authorizedDevs) == "" {
		d.Outcome = AuthorizationNoRestrictions
		return d
	}
	users := splitCSV(authorizedDevs)
	d.AuthorizedUsers = users
	for _, u := range users {
		if u == actor {
			d.Outcome = AuthorizationAllowed
			return d
		}
	}
	d.Outcome = AuthorizationDenied
	return d
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

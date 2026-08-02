// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package planfile feeds CLI flags from a typed plan JSON file, so
// workflow-computed values live in one reviewable document instead of
// being threaded through per-step env blocks.
//
// The plan file is named by $REUSABLE_CI_PLAN and holds one object per
// command scope, mapping flag names to values:
//
//	{
//	  "release assemble-dist": {"artifact-names": "build-a\nbuild-b", "prune-dirs": "true"},
//	  "container build":       {"context": ".", "platform": "linux/amd64"}
//	}
//
// Precedence is fixed and single: explicit flag > plan-file field >
// environment variable > flag default. That order falls out of source
// ordering — a plan source is prepended to the flag's env chain, and
// urfave/cli consults sources only when the flag was not set on the
// command line.
package planfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"

	"github.com/urfave/cli/v3"
)

// EnvVar names the environment variable holding the plan file path.
const EnvVar = "REUSABLE_CI_PLAN"

// warnedPaths remembers plan paths already reported as unreadable or
// malformed, so a broken plan warns once instead of once per flag.
var warnedPaths sync.Map //nolint:gochecknoglobals // once-per-path warn guard.

// loggedPaths remembers plan paths whose use has been announced, so the
// audit line (path + content digest) appears once per run, not per flag.
var loggedPaths sync.Map //nolint:gochecknoglobals // once-per-path audit log.

// Chain returns fallback with a plan-file source for (scope, key)
// resolved FIRST, giving flag > plan > env > default.
func Chain(scope, key string, fallback cli.ValueSourceChain) cli.ValueSourceChain {
	fallback.Chain = append([]cli.ValueSource{source{scope: scope, key: key}}, fallback.Chain...)

	return fallback
}

// Vars is Chain over a plain env-var list, for flags that do not already
// use a cienv chain.
func Vars(scope, key string, envNames ...string) cli.ValueSourceChain {
	return Chain(scope, key, cli.EnvVars(envNames...))
}

type source struct {
	scope string
	key   string
}

// Scope exposes the command path this source reads, for the guard test
// that pins every plan source's scope to its command's real path.
func (s source) Scope() string { return s.scope }

// PlanKey exposes the plan key this source reads, for the same guard.
func (s source) PlanKey() string { return s.key }

// Lookup reads the plan named by $REUSABLE_CI_PLAN and resolves
// [scope][key]. Any absence (no env var, no file, no scope, no key) is a
// clean miss so the next source in the chain wins; an unreadable or
// malformed plan warns once and is treated as absent rather than
// silently failing the whole command.
func (s source) Lookup() (string, bool) {
	path := os.Getenv(EnvVar)
	if path == "" {
		return "", false
	}

	raw, err := os.ReadFile(path) //nolint:gosec // path is the caller's own plan file.
	if err != nil {
		warnOnce(path, "plan file unreadable", err)

		return "", false
	}

	// Audit line: which plan fed this run, verifiable by digest.
	if _, loaded := loggedPaths.LoadOrStore(path, true); !loaded {
		sum := sha256.Sum256(raw)
		slog.Info("using plan file", "path", path, "sha256", hex.EncodeToString(sum[:]))
	}

	var plan map[string]map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		warnOnce(path, "plan file is not a {scope: {flag: value}} JSON object", err)

		return "", false
	}

	value, ok := plan[s.scope][s.key]
	if !ok {
		return "", false
	}

	str, ok := stringify(value)
	if !ok {
		// A structured value can't feed a scalar flag; a plan produced by
		// `plan write` never contains one, so this is a stale or foreign
		// plan — warn instead of silently missing.
		warnOnce(path+"#"+s.scope+"."+s.key, "plan value is not a scalar; ignoring", nil)

		return "", false
	}

	return str, true
}

func (s source) String() string {
	return fmt.Sprintf("plan file field %q of scope %q ($%s)", s.key, s.scope, EnvVar)
}

func (s source) GoString() string {
	return fmt.Sprintf("&planfile.source{scope:%q,key:%q}", s.scope, s.key)
}

func warnOnce(path, msg string, err error) {
	if _, loaded := warnedPaths.LoadOrStore(path, true); !loaded {
		slog.Warn(msg, "path", path, "err", err)
	}
}

// stringify renders a plan JSON value as the flag string urfave/cli
// parses: strings pass through, numbers and booleans use their JSON
// forms, and anything structured is a clean miss (flags are scalar).
func stringify(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	default:
		return "", false
	}
}

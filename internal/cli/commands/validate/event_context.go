// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func eventContextCmd() *cli.Command {
	return &cli.Command{
		Name:  "event-context",
		Usage: "refuse to run when the workflow trigger is outside the publish/release allowlist",
		Description: "Defense-in-depth guard placed at the entry of every privileged " +
			"publish/release workflow. Reads GITHUB_EVENT_NAME and refuses any trigger " +
			"outside the allowlist — most importantly the pull_request* family, which " +
			"would otherwise run with the caller's signing/package/API secrets attached " +
			"to PR-HEAD code. Default allowlist: push, workflow_dispatch, release, " +
			"schedule, workflow_run, merge_group. Adopters with legitimate PR-context " +
			"publish needs (preview deploys) override via --allowed-events on the step.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "event-name",
				Sources: cli.EnvVars("GITHUB_EVENT_NAME"),
				Usage:   "trigger event being checked; usually read from GITHUB_EVENT_NAME",
			},
			&cli.StringFlag{
				Name:    "allowed-events",
				Sources: cli.EnvVars("ALLOWED_EVENTS"),
				Usage:   "comma/space/newline-separated allowlist override (default: push,workflow_dispatch,release,schedule,workflow_run,merge_group)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.EventContext(os.Stderr, deps.Annotator(cmd), appvalidate.EventContextInput{
				EventName:     cmd.String("event-name"),
				AllowedEvents: splitAllowedEvents(cmd.String("allowed-events")),
			})
		},
	}
}

// splitAllowedEvents tolerates the same separators the workflow caller
// might naturally use — comma, space, newline, tab. Returns nil for
// empty input so the app layer falls back to DefaultAllowedEvents.
func splitAllowedEvents(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var out []string

	for _, line := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	}) {
		name := strings.TrimSpace(line)
		if name != "" {
			out = append(out, name)
		}
	}

	return out
}

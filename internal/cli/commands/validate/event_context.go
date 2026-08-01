// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/listval"
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
			"publish needs (preview deploys) override via --allowed-events on the step.\n\n" +
			"EXAMPLE:\n" +
			"   # Reads $FORGEJO_EVENT_NAME / $GITHUB_EVENT_NAME; refuses pull_request* and other non-allowlisted triggers\n" +
			"   reusable-ci validate event-context",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "event-name",
				Sources: cli.EnvVars("FORGEJO_EVENT_NAME", "GITHUB_EVENT_NAME"),
				Usage:   "trigger event being checked; read from $FORGEJO_EVENT_NAME / $GITHUB_EVENT_NAME on CI",
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
	return listval.Tokens(raw)
}

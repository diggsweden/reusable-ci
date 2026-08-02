// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func baseImagesFreshnessCmd() *cli.Command {
	return &cli.Command{
		Name:  "freshness",
		Usage: "compare pinned base-image digests with their upstream source tags",
		Description: `Resolves each check's moving source tag in the registry and compares it
with the digest the repo actually pins, emitting a stale-count output.
Config errors (unpinned refs, malformed source tags) always fail.

--enforce=true is the scheduled-cron mode: stale or unfetchable digests
fail the run. --enforce=false is the release-path mode: pins are for
reproducibility, so drift and registry blips only warn — currency stays
enforced out of band by the cron and renovate.

EXAMPLE:
   reusable-ci container base-images freshness --enforce=false --checks-json '[
     {"name":"DEBIAN_IMAGE",
      "pinned-ref":"docker.io/library/debian:trixie-slim@sha256:<hex>",
      "source-tag":"docker.io/library/debian:trixie-slim"}]'`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "checks-json", Required: true, Sources: cli.EnvVars("FRESHNESS_CHECKS_JSON"), Usage: `JSON array of checks: [{"name":..., "pinned-ref":..., "source-tag":...}]`},
			&cli.BoolFlag{Name: "enforce", Value: true, Sources: cli.EnvVars("FRESHNESS_ENFORCE"), Usage: "fail on stale or unfetchable digests (false: warn only; config errors still fail)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				return appcontainer.CheckFreshness(ctx, ociregistry.New(), dep.OutputSink, os.Stdout, os.Stderr, appcontainer.CheckFreshnessInput{
					ChecksJSON: cmd.String("checks-json"),
					Enforce:    cmd.Bool("enforce"),
				})
			})
		},
	}
}

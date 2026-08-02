// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func baseGraphGroup() *cli.Command {
	return &cli.Command{
		Name:  "base-graph",
		Usage: "compute deterministic base-image input IDs and build groups from a caller-provided graph",
		Description: `Reads a consumer-owned base graph JSON file and computes the
same content IDs, manifest input IDs, and build groups that shell workflows need
for base-image cache decisions. The graph schema stays caller-owned; reusable-ci
only owns deterministic hashing, Containerfile fragment extraction, and JSON/list
plumbing.`,
		Commands: []*cli.Command{
			baseGraphInputsCmd(),
			baseGraphInputSetIDCmd(),
			baseGraphFlavorCmd(),
			baseGraphGroupsJSONCmd(),
			baseGraphGroupFlavorsCmd(),
			baseGraphGroupContextFilesCmd(),
			baseGraphGroupsForMissingCmd(),
		},
	}
}

func baseGraphInputsCmd() *cli.Command {
	return &cli.Command{
		Name:  "inputs",
		Usage: "print JSON mapping every base flavor to content and manifest input IDs",
		Flags: baseGraphInputFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			result, err := appcontainer.BaseGraphInputs(baseGraphInputFromCmd(cmd))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, result.InputsJSON)

			return nil
		},
	}
}

func baseGraphInputSetIDCmd() *cli.Command {
	return &cli.Command{
		Name:  "input-set-id",
		Usage: "print the sha256 digest of the full base input JSON set",
		Flags: baseGraphInputFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			result, err := appcontainer.BaseGraphInputs(baseGraphInputFromCmd(cmd))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, result.InputSetID)

			return nil
		},
	}
}

func baseGraphFlavorCmd() *cli.Command {
	return &cli.Command{
		Name:  flagFlavor,
		Usage: "print one flavor's base-input-id or content-id",
		Flags: append(baseGraphInputFlags(),
			&cli.StringFlag{Name: flagFlavor, Required: true, Sources: cli.EnvVars("BASE_GRAPH_FLAVOR", "FLAVOR"), Usage: "base flavor to select"},
			&cli.StringFlag{Name: "field", Value: flagBaseInputID, Sources: cli.EnvVars("BASE_GRAPH_FIELD"), Usage: "field to print: base-input-id or content-id"},
		),
		Action: func(_ context.Context, cmd *cli.Command) error {
			result, err := appcontainer.BaseGraphSingleInput(baseGraphInputFromCmd(cmd), cmd.String(flagFlavor))
			if err != nil {
				return err
			}

			switch cmd.String("field") {
			case flagBaseInputID:
				_, _ = fmt.Fprintln(os.Stdout, result.BaseInputID)
			case flagContentID:
				_, _ = fmt.Fprintln(os.Stdout, result.ContentID)
			default:
				return fmt.Errorf("base graph: unsupported flavor field: %s: %w", cmd.String("field"), errs.ErrUsage)
			}

			return nil
		},
	}
}

func baseGraphGroupsJSONCmd() *cli.Command {
	return &cli.Command{
		Name:  flagGroupsJSON,
		Usage: "print base build groups JSON from the graph",
		Flags: baseGraphFileFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			groupsJSON, err := appcontainer.BaseGraphGroups(cmd.String("root"), cmd.String("graph-file"))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, groupsJSON)

			return nil
		},
	}
}

func baseGraphGroupFlavorsCmd() *cli.Command {
	return &cli.Command{
		Name:  "group-flavors",
		Usage: "print the flavors in one base build group, one per line",
		Flags: append(baseGraphFileFlags(), &cli.StringFlag{Name: "group", Required: true, Sources: cli.EnvVars("BASE_GRAPH_GROUP", "FLAVOR_GROUP"), Usage: "base build group"}),
		Action: func(_ context.Context, cmd *cli.Command) error {
			flavors, err := appcontainer.BaseGraphGroupFlavors(baseGraphGroupInputFromCmd(cmd))
			if err != nil {
				return err
			}

			for _, flavor := range flavors {
				_, _ = fmt.Fprintln(os.Stdout, flavor)
			}

			return nil
		},
	}
}

func baseGraphGroupContextFilesCmd() *cli.Command {
	return &cli.Command{
		Name:  "group-context-files",
		Usage: "print extra build-context files for one base build group, one per line",
		Flags: append(baseGraphFileFlags(), &cli.StringFlag{Name: "group", Required: true, Sources: cli.EnvVars("BASE_GRAPH_GROUP", "FLAVOR_GROUP"), Usage: "base build group"}),
		Action: func(_ context.Context, cmd *cli.Command) error {
			files, err := appcontainer.BaseGraphGroupContextFiles(baseGraphGroupInputFromCmd(cmd))
			if err != nil {
				return err
			}

			for _, file := range files {
				_, _ = fmt.Fprintln(os.Stdout, file)
			}

			return nil
		},
	}
}

func baseGraphGroupsForMissingCmd() *cli.Command {
	return &cli.Command{
		Name:  "groups-for-missing",
		Usage: "print JSON array of groups containing at least one missing flavor",
		Flags: append(baseGraphFileFlags(),
			&cli.StringFlag{Name: flagGroupsJSON, Sources: cli.EnvVars("BASE_BUILD_GROUPS_JSON"), Usage: "optional groups JSON array; when set, no graph file is read"},
			&cli.StringFlag{Name: "missing-json", Sources: cli.EnvVars("MISSING_FLAVORS_JSON"), Usage: "JSON array of missing flavor names"},
			&cli.StringSliceFlag{Name: "missing-flavor", Sources: cli.EnvVars("MISSING_FLAVOR"), Usage: "missing flavor name (repeatable or comma/space-separated via env)"},
		),
		Action: func(_ context.Context, cmd *cli.Command) error {
			groupsJSON, err := appcontainer.BaseGraphGroupsForMissing(appcontainer.BaseGraphGroupsForMissingInput{
				Root:          cmd.String("root"),
				GraphFile:     cmd.String("graph-file"),
				GroupsJSON:    cmd.String(flagGroupsJSON),
				MissingFlavor: cmd.StringSlice("missing-flavor"),
				MissingJSON:   cmd.String("missing-json"),
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, groupsJSON)

			return nil
		},
	}
}

func baseGraphFileFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "root", Value: ".", Sources: cli.EnvVars("BASE_GRAPH_ROOT"), Usage: "workspace root containing the graph and inputs"},
		&cli.StringFlag{Name: "graph-file", Value: appcontainer.DefaultBaseGraphFile, Sources: cli.EnvVars("BASE_GRAPH_FILE"), Usage: "base graph JSON path relative to --root"},
	}
}

func baseGraphInputFlags() []cli.Flag {
	return append(baseGraphFileFlags(),
		&cli.StringFlag{Name: flagContainerfile, Value: appcontainer.DefaultBaseGraphContainerfile, Sources: cli.EnvVars("BASE_GRAPH_CONTAINERFILE", "CONTAINERFILE"), Usage: "Containerfile path relative to --root"},
		&cli.StringSliceFlag{Name: flagArch, Sources: cli.EnvVars("BASE_GRAPH_ARCH_SET", "ARCH_SET"), Usage: "base architecture in the manifest input set (repeatable or comma/space-separated); defaults to amd64"},
	)
}

func baseGraphInputFromCmd(cmd *cli.Command) appcontainer.BaseGraphInput {
	return appcontainer.BaseGraphInput{
		Root:          cmd.String("root"),
		GraphFile:     cmd.String("graph-file"),
		Containerfile: cmd.String(flagContainerfile),
		ArchSet:       cmd.StringSlice(flagArch),
	}
}

func baseGraphGroupInputFromCmd(cmd *cli.Command) appcontainer.BaseGraphGroupInput {
	return appcontainer.BaseGraphGroupInput{
		Root:      cmd.String("root"),
		GraphFile: cmd.String("graph-file"),
		Group:     cmd.String("group"),
	}
}

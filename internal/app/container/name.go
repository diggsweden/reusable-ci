// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package container holds the use-case orchestration for the
// `reusable-ci container ...` subcommands. Imports domain only;
// adapter access (output sinks) goes through *deps.Deps.
package container

import (
	"context"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/container"
)

// ResolveNameInput drives the `reusable-ci container resolve-name` use case.
type ResolveNameInput struct {
	Registry        string
	ImageName       string
	Repository      string
	RepositoryOwner string
	Name            string
}

// ResolveName computes the canonical image reference and writes it to the
// output sink as `name=<value>`. Mirrors the
// scripts/container/resolve-image-name.sh contract.
func ResolveName(ctx context.Context, sink ci.OutputSink, in ResolveNameInput) error {
	resolved := container.ResolveImageName(container.ResolveImageNameInput{
		Registry:        in.Registry,
		ImageName:       in.ImageName,
		Repository:      in.Repository,
		RepositoryOwner: in.RepositoryOwner,
		Name:            in.Name,
	})
	return sink.Set(ctx, "name", resolved)
}

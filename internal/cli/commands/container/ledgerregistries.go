// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// cleanupRegistry composes the digest resolver (docker, OCI-generic) and
// the tag deleter (the forge provider, via its package API) into
// imageledger.CleanupRegistry. Deletion is forge-specific on purpose:
// staging and final tags share one manifest (digest-preserving promote),
// so a generic OCI/skopeo delete would destroy the promoted image — only
// the forge's tag-scoped package-API delete is safe.
type cleanupRegistry struct {
	resolver imageledger.DigestResolver
	deleter  provider.TagDeleter
}

func (r cleanupRegistry) ResolveDigest(ctx context.Context, ref string) (string, error) {
	return r.resolver.ResolveDigest(ctx, ref)
}

func (r cleanupRegistry) DeleteTag(ctx context.Context, ref string) error {
	return r.deleter.DeleteTag(ctx, ref)
}

type promotionRollbackRegistry struct {
	imageledger.Registry
	deleter provider.TagDeleter
}

func (r promotionRollbackRegistry) DeleteTag(ctx context.Context, ref string) error {
	return r.deleter.DeleteTag(ctx, ref)
}

// cleanupReg builds the registry the cleanup flow runs against. In
// dry-run it previews via the in-memory decorator (no forge needed); a
// real run resolves digests via docker and deletes via the active
// forge's tag-scoped package API (gated by RequireTagDeleter).
func cleanupReg(d *deps.Deps, dryRun bool) (imageledger.CleanupRegistry, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if dryRun {
		return newDryRunRegistry(ociregistry.New(), os.Stderr), nil
	}

	deleter, err := d.RequireTagDeleter()
	if err != nil {
		return nil, err
	}

	// Narrate each real deletion to stderr (audit trail), matching the
	// per-tag visibility the dry-run path already gives.
	return auditDeleter{
		CleanupRegistry: cleanupRegistry{resolver: ociregistry.New(), deleter: deleter},
		out:             os.Stderr,
	}, nil
}

func promotionRollbackReg(d *deps.Deps, dryRun bool) (imageledger.PromotionRollbackRegistry, error) { //nolint:varnamelen // idiomatic short name.
	if dryRun {
		return newDryRunRegistry(ociregistry.New(), os.Stderr), nil
	}

	deleter, err := d.RequireTagDeleter()
	if err != nil {
		return nil, err
	}

	return auditPromotionRollbackRegistry{
		PromotionRollbackRegistry: promotionRollbackRegistry{Registry: ociregistry.New(), deleter: deleter},
		out:                       os.Stderr,
	}, nil
}

// dryRunRegistry previews ledger mutations without touching the
// registry: digest reads pass through to the wrapped resolver, a CopyTag
// is simulated in-memory (so the domain's post-copy digest verification
// still runs and passes), and both CopyTag/DeleteTag are logged. It
// implements imageledger.Registry and imageledger.CleanupRegistry.
type dryRunRegistry struct {
	resolver  imageledger.DigestResolver
	out       io.Writer
	simulated map[string]string
}

func newDryRunRegistry(resolver imageledger.DigestResolver, out io.Writer) *dryRunRegistry {
	return &dryRunRegistry{resolver: resolver, out: out, simulated: map[string]string{}}
}

func (d *dryRunRegistry) ResolveDigest(ctx context.Context, ref string) (string, error) {
	if dig, ok := d.simulated[ref]; ok {
		return dig, nil
	}

	return d.resolver.ResolveDigest(ctx, ref)
}

func (d *dryRunRegistry) CopyTag(ctx context.Context, source, dest string) error {
	dig, err := d.resolver.ResolveDigest(ctx, source)
	if err != nil {
		return err
	}

	d.simulated[dest] = dig
	_, _ = fmt.Fprintf(d.out, "[dry-run] would promote %s -> %s (%s)\n", source, dest, dig)

	return nil
}

func (d *dryRunRegistry) DeleteTag(_ context.Context, ref string) error {
	_, _ = fmt.Fprintf(d.out, "[dry-run] would delete %s\n", ref)

	return nil
}

// CopyWithSignatures simulates a cross-registry promotion (the real run
// uses cosign copy). It lands the source's digest at dest in the simulated
// map so the domain's post-copy digest verification still runs and passes.
func (d *dryRunRegistry) CopyWithSignatures(ctx context.Context, source, dest string) error {
	dig, err := d.resolver.ResolveDigest(ctx, source)
	if err != nil {
		return err
	}

	d.simulated[dest] = dig
	_, _ = fmt.Fprintf(d.out, "[dry-run] would cross-registry promote (with signatures) %s -> %s (%s)\n", source, dest, dig)

	return nil
}

// cosignSigCopier adapts the cosign adapter to imageledger.SignatureCopier:
// it narrates each cross-registry copy (matching the same-repo audit trail)
// and supplies the redaction sink. Used for real cross-registry promotion,
// where the registry-attached signature must travel with the image.
type cosignSigCopier struct {
	cosign *cosign.Adapter
	out    io.Writer
}

func (c cosignSigCopier) CopyWithSignatures(ctx context.Context, source, dest string) error {
	_, _ = fmt.Fprintf(c.out, "ledger: cross-registry promoting (with signatures) %s -> %s\n", source, dest)

	return c.cosign.CopyImage(ctx, cosign.CopyImageInput{Source: source, Dest: dest}, c.out)
}

// auditCopier wraps the real promote registry and narrates each CopyTag
// to out before delegating. Mirrors dryRunRegistry's preview narration so
// a non-dry-run promotion leaves the same per-tag audit trail in the CI
// log that --dry-run shows — destructive registry mutations should be at
// least as observable as their preview. ResolveDigest passes through the
// embedded interface unlogged.
type auditCopier struct {
	imageledger.Registry

	out io.Writer
}

func (a auditCopier) CopyTag(ctx context.Context, source, dest string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: promoting %s -> %s\n", source, dest)

	return a.Registry.CopyTag(ctx, source, dest)
}

// auditDeleter is the cleanup/rollback counterpart: it narrates each real
// DeleteTag before delegating, so the actually-destructive deletion is as
// observable as the [dry-run] preview.
type auditDeleter struct {
	imageledger.CleanupRegistry

	out io.Writer
}

func (a auditDeleter) DeleteTag(ctx context.Context, ref string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: deleting %s\n", ref)

	return a.CleanupRegistry.DeleteTag(ctx, ref)
}

type auditPromotionRollbackRegistry struct {
	imageledger.PromotionRollbackRegistry

	out io.Writer
}

func (a auditPromotionRollbackRegistry) CopyTag(ctx context.Context, source, dest string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: restoring %s -> %s\n", source, dest)

	return a.PromotionRollbackRegistry.CopyTag(ctx, source, dest)
}

func (a auditPromotionRollbackRegistry) DeleteTag(ctx context.Context, ref string) error {
	_, _ = fmt.Fprintf(a.out, "ledger: deleting %s\n", ref)

	return a.PromotionRollbackRegistry.DeleteTag(ctx, ref)
}

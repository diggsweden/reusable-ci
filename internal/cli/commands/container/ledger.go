// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// defaultLedgerBasename is the conventional release-image ledger filename;
// defaultLedgerPath places it under the GoReleaser dist layout. Overridable
// via --ledger. The basename is also what `ledger merge` looks for when handed
// a directory of downloaded per-container ledger artifacts.
const (
	defaultLedgerBasename = "release-images.json"
	defaultLedgerPath     = "dist/" + defaultLedgerBasename
)

// ledgerGroup wires `container ledger`: the digest-first release-image
// ledger — record image entries from the build stage and re-validate the
// ledger at the sign/publish trust boundary. Forge-agnostic (pure OCI
// refs/digests/tags); the Go port of forgejo-ci's record-release-image.
func ledgerGroup() *cli.Command {
	return &cli.Command{
		Name:  "ledger",
		Usage: "release-image ledger: record image entries and re-validate at the trust boundary",
		Commands: []*cli.Command{
			ledgerAddCmd(),
			ledgerMergeCmd(),
			ledgerValidateCmd(),
			ledgerVerifyDigestsCmd(),
			ledgerPromoteCmd(),
			ledgerCleanupCmd(),
			ledgerRollbackCmd(),
		},
	}
}

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

// ledgerMutation is a cleanup-style domain op (cleanup / rollback) over
// a CleanupRegistry.
type ledgerMutation func(context.Context, imageledger.CleanupRegistry, []imageledger.Entry, string) error

// runLedgerMutation is the shared body for the cleanup/rollback verbs:
// read + parse the ledger, build the registry (real or dry-run), run the
// op, and report. doneVerb completes "ledger: <doneVerb> N entr(y/ies)".
func runLedgerMutation(ctx context.Context, cmd *cli.Command, op ledgerMutation, doneVerb string) error {
	return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		data, err := cliio.ReadFile(cmd.String("ledger"))
		if err != nil {
			return fmt.Errorf("ledger: read %s: %w", cmd.String("ledger"), err)
		}

		entries, err := imageledger.Parse(data)
		if err != nil {
			return err
		}

		reg, err := cleanupReg(d, cmd.Bool("dry-run"))
		if err != nil {
			return err
		}

		if err := op(ctx, reg, entries, cmd.String("tag")); err != nil {
			return err
		}

		_, _ = fmt.Fprintf(os.Stderr, "ledger: %s %d entr(y/ies) (release tag %q)\n", doneVerb, len(entries), cmd.String("tag"))

		return nil
	})
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

// dryRunFlag previews registry mutations without performing them.
func dryRunFlag() cli.Flag {
	// Long-flag only: the rest of the CLI exposes no short flags, so a lone
	// -n would imply a short-flag convention that does not exist elsewhere.
	return &cli.BoolFlag{Name: "dry-run", Usage: "preview registry mutations (copies/deletes) without performing them"}
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

func releaseTagFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "tag",
		Sources: cienv.Tag(),
		Usage:   "release tag that final_tag (and candidate_tag) must be scoped to",
	}
}

func stageFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "stage",
		Value:   imageledger.ReleaseStageName,
		Sources: cli.EnvVars("PROMOTE_STAGE"),
		Usage:   "promotion stage: 'release' adds the <base>:release pointer and enforces the release scope; a named stage ('dev', 'staging') adds <base>:<stage> on the same digest and needs no --tag. The immutable :<version> tag is build-only.",
	}
}

func stageRepoFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "stage-repo",
		Sources: cli.EnvVars("PROMOTE_STAGE_REPO"),
		Usage:   "rehome the promotion onto a destination registry/namespace PREFIX (e.g. a sovereign codeberg.org/owner); each image lands at <prefix>/<image-name>, so a multi-container release never collides. A different registry is a cross-registry promotion that copies the signature via cosign",
	}
}

func ledgerPathFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "ledger",
		Value:   defaultLedgerPath,
		Sources: cli.EnvVars("RELEASE_IMAGES_LEDGER"),
		Usage:   "ledger JSON file (a bare array; created if absent on add)",
	}
}

func ledgerAddCmd() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "validate one image entry and append it to the ledger",
		Description: `EXAMPLES:
   # Record a pushed image, capturing its digest from the registry
   reusable-ci container ledger add --kind distroless \
     --candidate-tag codeberg.org/owner/repo:staging-v1.2.3 \
     --final-tag codeberg.org/owner/repo:v1.2.3 \
     --sbom dist/image-sbom.cyclonedx.json --tag v1.2.3 --capture-digest

   # Direct-push consumer with an explicit digest
   reusable-ci container ledger add --kind alpine \
     --ref codeberg.org/owner/repo@sha256:… --digest sha256:… \
     --final-tag codeberg.org/owner/repo:v1.2.3-alpine \
     --sbom dist/image-sbom-alpine.cyclonedx.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			&cli.StringFlag{Name: "kind", Usage: "image role, e.g. distroless, alpine, base"},
			&cli.StringFlag{Name: "ref", Usage: "digest-pinned image ref (registry/path@sha256:<64 hex>)"},
			&cli.StringFlag{Name: flagDigest, Usage: "image digest (sha256:<64 hex>)"},
			&cli.StringFlag{Name: "sbom", Usage: "CycloneDX SBOM path (dist/image-sbom*.cyclonedx.json)"},
			&cli.StringFlag{Name: "image-name", Usage: "image registry/path (no tag); with --tag, derives --final-tag=<image>:<tag> and --candidate-tag=<image>:staging-<tag> so callers don't hand-assemble both (explicit flags still win)"}, //nolint:goconst // flag name; matches the package convention.
			&cli.StringFlag{Name: "final-tag", Usage: "immutable release tag ref (scoped to --tag); derived from --image-name when omitted"},
			&cli.StringFlag{Name: "flavor", Usage: "optional base-image flavour"},
			&cli.StringFlag{Name: "candidate-tag", Usage: "optional staging tag ref (scoped to staging-<tag>); derived from --image-name when omitted"},
			&cli.StringFlag{Name: "base-ref", Usage: "optional digest-pinned base image"},
			&cli.StringFlag{Name: "base-input-id", Usage: "optional base-image input identifier (SLSA lineage)"},
			&cli.BoolFlag{Name: "capture-digest", Usage: "resolve the digest from the registry (single source of truth) instead of --ref/--digest; reads the candidate or final tag"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path := cmd.String("ledger")

			finalTag, candidateTag := cmd.String("final-tag"), cmd.String("candidate-tag")
			// Single-source the final/candidate convention in Go: a build job can
			// pass --image-name + --tag and let the domain derive both refs the
			// validator enforces, instead of re-encoding ":<tag>"/":staging-<tag>"
			// in each forge's workflow. Explicit flags override.
			if image := cmd.String("image-name"); image != "" {
				dFinal, dCandidate := imageledger.DeriveTags(image, cmd.String("tag"))
				if finalTag == "" {
					finalTag = dFinal
				}

				if candidateTag == "" {
					candidateTag = dCandidate
				}
			}

			entry := imageledger.Entry{
				Kind:         cmd.String("kind"),
				Flavor:       cmd.String("flavor"),
				Ref:          cmd.String("ref"),
				Digest:       cmd.String(flagDigest),
				SBOM:         cmd.String("sbom"),
				FinalTag:     finalTag,
				CandidateTag: candidateTag,
				BaseRef:      cmd.String("base-ref"),
				BaseInputID:  cmd.String("base-input-id"),
			}

			// Digest capture hits the registry — do it before taking the
			// lock so we never hold the ledger lock across a network call.
			if cmd.Bool("capture-digest") {
				if err := captureEntryDigest(ctx, ociregistry.New(), &entry); err != nil {
					return err
				}
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // dist dir; 0755 is conventional.
				return fmt.Errorf("ledger: create dir for %s: %w", path, err)
			}

			// Serialise the read-modify-write under an exclusive file lock:
			// parallel `ledger add` against one ledger (e.g. several image
			// variants recorded concurrently) would otherwise clobber each
			// other and silently drop entries. Re-read inside the lock so
			// each writer sees the latest state.
			var added bool

			if err := cliio.WithLock(path, func() error {
				existing, _ := os.ReadFile(path) //nolint:gosec,errcheck // operator-supplied ledger path; an absent file means an empty ledger.

				out, wasNew, err := imageledger.Append(existing, entry, cmd.String("tag"))
				if err != nil {
					return err
				}

				added = wasNew

				return cliio.WriteFile(path, out, 0o644) //nolint:gosec // ledger is public release metadata, not a secret.
			}); err != nil {
				return err
			}

			// Report the truthful outcome: Append is idempotent, so a
			// retried add of an already-recorded entry is a no-op, not an
			// append.
			verb := "appended"
			if !added {
				verb = "already recorded; no change to"
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %s %s (%s) → %s\n", verb, entryDescriptor(entry.Kind), entry.Digest, path)

			return nil
		},
	}
}

// entryDescriptor renders the ledger entry's noun for the add message. --kind
// is optional, so an empty kind collapses to a bare "entry" rather than
// leaving a doubled space.
func entryDescriptor(kind string) string {
	if kind == "" {
		return "entry"
	}

	return kind + " entry"
}

// captureEntryDigest resolves the entry's image digest from the registry
// — the authoritative single source (no hand-typed digest, no
// transcription gap) — and rewrites Digest + the digest-pinned Ref from
// it. It reads whichever tag the consumer just pushed: the candidate tag
// (staging flow) when set, else the final tag. Matches the design the
// migration plan recommends (§8) over a consumer-supplied digest.
func captureEntryDigest(ctx context.Context, resolver imageledger.DigestResolver, entry *imageledger.Entry) error {
	src := entry.DigestSource()
	if src == "" {
		return fmt.Errorf("ledger add --capture-digest needs a candidate or final tag to resolve: %w", errs.ErrUsage)
	}

	digest, err := resolver.ResolveDigest(ctx, src)
	if err != nil {
		return fmt.Errorf("ledger add: capture digest from %s: %w", src, err)
	}

	entry.PinDigest(digest)

	return nil
}

func ledgerValidateCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "re-validate every entry in the ledger against the release tag (trust-boundary check)",
		Description: `EXAMPLE:
   reusable-ci container ledger validate --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String("ledger"))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String("ledger"), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			if err := imageledger.ValidateAll(entries, cmd.String("tag")); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) valid for release tag %q\n", len(entries), cmd.String("tag"))

			return nil
		},
	}
}

func ledgerVerifyDigestsCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify-digests",
		Usage: "re-verify each entry's recorded digest against what the registry serves (candidate_tag → final_tag → digest ref); distinct from `validate`, which checks entries against the release tag offline",
		Description: `EXAMPLE:
   reusable-ci container ledger verify-digests --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String("ledger"))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String("ledger"), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			if err := imageledger.Verify(ctx, ociregistry.New(), entries, cmd.String("tag")); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) verified against the registry for %q\n", len(entries), cmd.String("tag"))

			return nil
		},
	}
}

func ledgerPromoteCmd() *cli.Command {
	return &cli.Command{
		Name:  "promote",
		Usage: "promote each entry's candidate image to the stage's moving pointer (<base>:<stage>) on the same digest, verifying after each copy; the immutable :<version> tag is build-only",
		Description: `EXAMPLES:
   # Promote to the :release pointer (release scope requires --tag)
   reusable-ci container ledger promote --ledger release-images.json --tag v1.2.3 --stage release

   # Preview a staging promotion without touching the registry
   reusable-ci container ledger promote --ledger release-images.json --stage staging --dry-run`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			stageFlag(),
			stageRepoFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String("ledger"))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String("ledger"), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			stage := imageledger.Stage{Name: cmd.String("stage"), TargetRepo: cmd.String("stage-repo")}

			// Real run narrates each copy (audit trail); dry-run simulates +
			// previews. A cross-registry destination (--stage-repo) copies the
			// signature via cosign; same-repo promotions never consult it.
			var (
				reg       imageledger.Registry
				sigCopier imageledger.SignatureCopier
			)

			if cmd.Bool("dry-run") {
				dryRun := newDryRunRegistry(ociregistry.New(), os.Stderr)
				reg, sigCopier = dryRun, dryRun
			} else {
				reg = auditCopier{Registry: ociregistry.New(), out: os.Stderr}
				if stage.TargetRepo != "" {
					sigCopier = cosignSigCopier{cosign: cosign.New(), out: os.Stderr}
				}
			}

			if err := imageledger.PromoteToStage(ctx, reg, sigCopier, entries, cmd.String("tag"), stage); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: promoted %d entr(y/ies) to stage %q\n", len(entries), stage.Name)

			return nil
		},
	}
}

func ledgerCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  "cleanup",
		Usage: "delete each entry's staging candidate tag after verifying the promoted final tag (leaves candidates in place if unverified)",
		Description: `EXAMPLE:
   reusable-ci container ledger cleanup --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runLedgerMutation(ctx, cmd, imageledger.Cleanup, "cleaned up candidate tags for")
		},
	}
}

func ledgerRollbackCmd() *cli.Command {
	return &cli.Command{
		Name:  "rollback",
		Usage: "undo a stage's promotion: delete that stage's pointer tag(s) (e.g. :dev/:staging/:release) that still serve the entry's digest; the immutable :<version> tag is never touched",
		Description: `EXAMPLE:
   reusable-ci container ledger rollback --ledger release-images.json --tag v1.2.3 --stage release`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			stageFlag(),
			stageRepoFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				data, err := cliio.ReadFile(cmd.String("ledger"))
				if err != nil {
					return fmt.Errorf("ledger: read %s: %w", cmd.String("ledger"), err)
				}

				entries, err := imageledger.Parse(data)
				if err != nil {
					return err
				}

				reg, err := cleanupReg(d, cmd.Bool("dry-run"))
				if err != nil {
					return err
				}

				stage := imageledger.Stage{Name: cmd.String("stage"), TargetRepo: cmd.String("stage-repo")}
				if err := imageledger.Rollback(ctx, reg, entries, cmd.String("tag"), stage); err != nil {
					return err
				}

				_, _ = fmt.Fprintf(os.Stderr, "ledger: rolled back %d entr(y/ies) for stage %q\n", len(entries), stage.Name)

				return nil
			})
		},
	}
}

func ledgerMergeCmd() *cli.Command {
	return &cli.Command{
		Name:      "merge",
		Usage:     "merge several per-container ledger files into one (drops exact duplicates); for promoting a multi-container release as a single unit",
		ArgsUsage: "<path>... (files, or directories searched for release-images.json)",
		Description: `EXAMPLE:
   reusable-ci container ledger merge dist/container-a dist/container-b --ledger release-images.json`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			docs, err := readLedgerDocs(cmd.Args().Slice())
			if err != nil {
				return err
			}

			out, err := imageledger.Merge(docs)
			if err != nil {
				return err
			}

			// A merge that yields no entries means no ledger was found where one
			// was expected (e.g. the build's best-effort ledger upload didn't
			// run). Promotion will be a safe no-op, but surface it as a
			// forge-aware annotation (::warning:: on GitHub; a plain Warning:
			// line elsewhere, incl. Forgejo) so it is visible rather than a
			// silently green run.
			if entries, perr := imageledger.Parse(out); perr == nil && len(entries) == 0 {
				deps.Annotator(cmd).Warningf("ledger merge: no entries found under %v — promotion will be a no-op; check the build's ledger upload", cmd.Args().Slice())
			}

			path := cmd.String("ledger")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // dist dir; 0755 is conventional.
				return fmt.Errorf("ledger: create dir for %s: %w", path, err)
			}

			if err := cliio.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // ledger is public release metadata, not a secret.
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: merged %d source(s) → %s\n", len(docs), path)

			return nil
		},
	}
}

// readLedgerDocs reads ledger JSON from each path: a file is read directly, a
// directory is walked for files named release-images.json (the per-container
// artifacts the promotion job downloads). Missing inputs are an error so a
// silent empty merge can't mask a broken hand-off.
func readLedgerDocs(paths []string) ([][]byte, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("ledger merge: at least one input path is required: %w", errs.ErrMissingInput)
	}

	var docs [][]byte

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// A missing input degrades to "no ledger here" rather than a hard
				// failure: when the build's best-effort ledger upload didn't run,
				// the promotion job finds no artifacts and must no-op (the image
				// still ships with its build tags) instead of reddening a release
				// whose signed artifacts already published.
				_, _ = fmt.Fprintf(os.Stderr, "ledger merge: %s not found, skipping\n", path)

				continue
			}

			return nil, fmt.Errorf("ledger merge: %s: %w", path, err)
		}

		if info.IsDir() {
			found, walkErr := readLedgerDir(path)
			if walkErr != nil {
				return nil, walkErr
			}

			docs = append(docs, found...)

			continue
		}

		data, readErr := cliio.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("ledger merge: read %s: %w", path, readErr)
		}

		docs = append(docs, data)
	}

	return docs, nil
}

// readLedgerDir walks dir and reads every file named defaultLedgerBasename —
// the per-container ledger artifacts the promotion job downloads into one
// directory tree.
func readLedgerDir(dir string) ([][]byte, error) {
	var docs [][]byte

	walkErr := filepath.WalkDir(dir, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || entry.Name() != defaultLedgerBasename {
			return nil
		}

		data, readErr := os.ReadFile(filePath) //nolint:gosec // operator-supplied ledger directory.
		if readErr != nil {
			return fmt.Errorf("read %s: %w", filePath, readErr)
		}

		docs = append(docs, data)

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("ledger merge: walk %s: %w", dir, walkErr)
	}

	return docs, nil
}

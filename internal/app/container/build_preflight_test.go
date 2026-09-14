// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestOCIReleaseLabels_RejectsLineBreaksInEveryField(t *testing.T) {
	t.Parallel()

	fields := map[string]func(*appcontainer.OCIReleaseLabelsInput, string){
		"title":         func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Title = value },
		"version":       func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Version = value },
		"revision":      func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Revision = value },
		"ref":           func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.RefName = value },
		"source":        func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Source = value },
		"created":       func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Created = value },
		"documentation": func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Documentation = value },
		"description":   func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Description = value },
		"licenses":      func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Licenses = value },
		"vendor":        func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Vendor = value },
		"authors":       func(in *appcontainer.OCIReleaseLabelsInput, value string) { in.Authors = value },
	}
	for name, set := range fields {
		for _, control := range []string{"\r", "\n"} {
			t.Run(name+control, func(t *testing.T) {
				t.Parallel()

				in := appcontainer.OCIReleaseLabelsInput{OCILabels: domaincontainer.OCILabels{Title: "title", Version: "version", Revision: "revision", RefName: "ref"}, Source: "source", Created: "created"}
				set(&in, "value"+control+"--another-argument")
				flags, err := appcontainer.OCIReleaseLabelFlags(in)
				require.ErrorIs(t, err, errs.ErrUsage)
				require.Nil(t, flags)
			})
		}
	}
}

func TestMaterializeBuildSecrets_EmptyLateValueHasNoEffects(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	before := snapshotContainerTree(t, root)
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	err := appcontainer.MaterializeBuildSecrets(t.Context(), sink, &log, appcontainer.MaterializeBuildSecretsInput{Names: "FIRST,SECOND", EnvelopeJSON: `{"FIRST":"synthetic","SECOND":""}`, OutputDir: root})
	require.ErrorIs(t, err, errs.ErrInvalidConfig)
	require.Empty(t, sink.Keys())
	require.Empty(t, log.String())
	require.Equal(t, before, snapshotContainerTree(t, root))
}

func TestUsableImageDigest_EmptyLabelStillRequiresPresence(t *testing.T) {
	t.Parallel()

	for _, present := range []bool{false, true} {
		labels := map[string]string{}
		if present {
			labels["required"] = ""
		}

		registry := &fakeUsableImageRegistry{digest: oneDigest, arch: "amd64", labels: labels}
		sink := fakeoutputsink.New(t)

		got, err := appcontainer.UsableImageDigest(t.Context(), registry, sink, appcontainer.UsableImageDigestInput{Ref: "registry.example/app:tag", Arch: "amd64", RequiredLabels: []string{"required="}})
		if present {
			require.NoError(t, err)
			require.NotNil(t, got)
		} else {
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Nil(t, got)
			require.Empty(t, sink.Keys())
		}
	}
}

func buildPushPreflightInput(t *testing.T) appcontainer.BuildPushOCIImageInput {
	t.Helper()
	file := filepath.Join(t.TempDir(), "Containerfile")
	require.NoError(t, os.WriteFile(file, []byte("FROM scratch\n"), 0o600))

	return appcontainer.BuildPushOCIImageInput{Tag: "v1", Image: "registry.example/app", Containerfile: file, BuildsJSON: `[{"platform":"linux/amd64"}]`, TLSVerify: "true", ServerURL: "https://registry.example", Repository: "owner/app", OCILabels: domaincontainer.OCILabels{Title: "fixture", Revision: "revision"}, RetryAttempts: 1}
}

func TestBuildPushOCIImage_RejectsBadCoordinatesBeforeTools(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ tag, image string }{
		{":bad", "registry.example/app"}, {"bad\rflag", "registry.example/app"}, {"bad/tag", "registry.example/app"},
		{"v1", "registry.example/app:tag"}, {"v1", "registry.example/app@" + oneDigest}, {"v1", "registry.example/a/../app"},
	} {
		in := buildPushPreflightInput(t)
		in.Tag, in.Image = tc.tag, tc.image
		tool := &fakeBuildPushTool{digest: oneDigest}
		sink := fakeoutputsink.New(t)

		var log bytes.Buffer

		_, err := appcontainer.BuildPushOCIImage(t.Context(), tool, fakeBuildPushGit{epoch: "1700000000"}, sink, &log, in)
		require.ErrorIs(t, err, errs.ErrUsage)
		require.Empty(t, tool.builds)
		require.Empty(t, tool.pushes)
		require.Empty(t, log.String())
		require.Empty(t, sink.Keys())
	}
}

func TestBuildPublication_RefusesMalformedReturnedDigests(t *testing.T) {
	t.Parallel()

	for _, digest := range []string{"sha256:short", "not-a-digest", "sha512:wrong"} {
		t.Run(digest, func(t *testing.T) {
			t.Parallel()
			sink := fakeoutputsink.New(t)

			var log bytes.Buffer

			got, err := appcontainer.BuildImage(t.Context(), &fakeBuilder{}, &fakePusher{digest: digest}, sink, &log, appcontainer.BuildImageInput{Context: ".", Mode: domaincontainer.BuildModePushByDigest, ImageRef: "registry.example/app"})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Empty(t, got)
			require.Empty(t, sink.Keys())
			require.NotContains(t, log.String(), digest)
			log.Reset()
			result, err := appcontainer.BuildPushOCIImage(t.Context(), &fakeBuildPushTool{digest: digest}, fakeBuildPushGit{epoch: "1700000000"}, sink, &log, buildPushPreflightInput(t))
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Nil(t, result)
			require.Empty(t, sink.Keys())
			require.NotContains(t, log.String(), digest)
			require.NotContains(t, log.String(), "Pushed ")
		})
	}
}

func TestBuildImage_SecretGrammarRefusesBeforeBuilder(t *testing.T) {
	t.Parallel()

	builder := &fakeBuilder{}

	var log bytes.Buffer

	_, err := appcontainer.BuildImage(t.Context(), builder, nil, nil, &log, appcontainer.BuildImageInput{Context: ".", Mode: domaincontainer.BuildModeLoad, ImageRef: "registry.example/app:tag", Secrets: []string{"id=first,src=fixture", "id=late"}})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Empty(t, builder.got.Context)
	require.Empty(t, log.String())
}

func TestSetupBuildah_RefusesPathsOutsideJobBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"storage", "config", "temp", "env", "linked parent", "job root", "config escape"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			job := filepath.Join(root, "job")
			outside := filepath.Join(root, "outside")

			require.NoError(t, os.Mkdir(job, 0o700))
			require.NoError(t, os.Mkdir(outside, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), []byte("canary"), 0o600))

			in := appcontainer.SetupBuildahInput{RunnerTemp: job, InstallPackages: true}

			switch field {
			case "config escape":
				in.StorageRoot = filepath.Join(job, `store\u002f..`)
			case "job root":
				in.StorageRoot = job
			case "storage":
				in.StorageRoot = outside
			case "config":
				in.StorageConf = filepath.Join(outside, "keep")
			case "temp":
				in.TmpDir = outside
			case "env":
				in.EnvFile = filepath.Join(outside, "keep")
			case "linked parent":
				require.NoError(t, os.Symlink(outside, filepath.Join(job, "link")))
				in.StorageRoot = filepath.Join(job, "link", "store")
			}

			before := snapshotContainerTree(t, root)
			tool := &fakeBuildahSetupTool{commands: map[string]bool{"buildah": true, "apt-get": true}}
			installer := &fakePackageInstaller{}
			sink := fakeoutputsink.New(t)

			var log bytes.Buffer

			_, err := appcontainer.SetupBuildah(t.Context(), tool, installer, sink, nil, &log, in)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.Empty(t, installer.packages)
			require.Empty(t, tool.calls)
			require.Empty(t, log.String())
			require.Empty(t, sink.Keys())
			require.Equal(t, before, snapshotContainerTree(t, root))
		})
	}
}

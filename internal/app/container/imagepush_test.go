// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeImagePushTool struct {
	pushes    []string
	failures  int
	digest    string
	tlsVerify bool
}

func (f *fakeImagePushTool) PushImageToRefWithDigest(_ context.Context, authFile string, tlsVerify bool, image, destination string, _ io.Writer) (string, error) {
	f.pushes = append(f.pushes, authFile+"|"+image+"|"+destination)

	f.tlsVerify = tlsVerify
	if f.failures > 0 {
		f.failures--

		return "", errors.New("temporary push failure") //nolint:err113 // test double error.
	}

	return f.digest, nil
}

// TestPushImage_ReportsTheDigestTheRegistryHolds covers a push that succeeds on
// the second attempt: the digest reported back is the one computed from the
// manifest the registry actually serves, and it is published as an output
// alongside the pinned reference.
//
// TestPushImage_RejectsDigestMismatch covers the other half -- what happens
// when the pushed digest and the registry's disagree.
func TestPushImage_ReportsTheDigestTheRegistryHolds(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	digest := manifestTestDigest(raw)
	tool := &fakeImagePushTool{failures: 1, digest: digest}
	registry := &fakeManifestPushRegistry{failures: 1, raw: raw}
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	got, err := appcontainer.PushImage(context.Background(), tool, registry, sink, &log, appcontainer.PushImageInput{
		LocalImage:    "localhost/app:arch",
		Destination:   "registry.example/app:staging-amd64",
		AuthFile:      "auth.json",
		TLSVerify:     "true",
		RetryAttempts: 2,
		RetryDelay:    time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Digest != digest {
		t.Errorf("digest = %q, want the digest of the manifest the registry returned %q", got.Digest, digest)
	}

	if want := "registry.example/app:staging-amd64@" + digest; got.Ref != want {
		t.Errorf("ref = %q, want %q", got.Ref, want)
	}

	if v := sink.Single("digest"); v != digest {
		t.Errorf("output digest = %q, want %q", v, digest)
	}

	if v := sink.Single("ref"); v != got.Ref {
		t.Errorf("output ref = %q, want it to match the returned ref %q", v, got.Ref)
	}

	// The double records auth file, source and destination; counting the
	// pushes said nothing about where they went or what they carried.
	if want := []string{"auth.json|localhost/app:arch|registry.example/app:staging-amd64", "auth.json|localhost/app:arch|registry.example/app:staging-amd64"}; !reflect.DeepEqual(tool.pushes, want) {
		t.Errorf("pushes = %v, want the same push retried once: %v", tool.pushes, want)
	}

	if !tool.tlsVerify {
		t.Error("pushed without TLS verification")
	}

	if !strings.Contains(log.String(), "attempt 1/2") {
		t.Fatalf("log missing retry message: %q", log.String())
	}
}

// TestPushImage_TreatsThePushedDigestAsAnExpectation covers the three states
// the digest buildah reports can be in. The digest that is published is always
// the one computed from the manifest the registry serves; what the push
// reported is checked against it.
//
// Unlike the manifest resolution there is no "not configured" state -- a push
// always reports something -- which is why this path warns unconditionally on
// an unusable value. The quiet case is asserted all the same: the ordinary
// push must not tell an operator its digest was invalid.
func TestPushImage_TreatsThePushedDigestAsAnExpectation(t *testing.T) {
	raw := []byte(`{"schemaVersion":2}`)
	registryDigest := manifestTestDigest(raw)

	for name, testCase := range map[string]struct {
		pushed   string
		wantErr  error
		wantWarn bool
	}{
		"push agrees with the registry": {pushed: registryDigest, wantWarn: false},
		"push reported nothing usable":  {pushed: "", wantWarn: true},
		"push reported another image":   {pushed: "sha256:" + strings.Repeat("0", 64), wantErr: errs.ErrValidation},
	} {
		t.Run(name, func(t *testing.T) {
			var log bytes.Buffer

			got, err := appcontainer.PushImage(context.Background(),
				&fakeImagePushTool{digest: testCase.pushed},
				&fakeManifestPushRegistry{raw: raw},
				fakeoutputsink.New(t), &log, appcontainer.PushImageInput{
					LocalImage:  "localhost/app:arch",
					Destination: "registry.example/app:staging-amd64",
					TLSVerify:   "true",
				})

			if testCase.wantErr != nil {
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("err = %v, want %v", err, testCase.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.Digest != registryDigest {
				t.Errorf("digest = %q, want the registry's %q", got.Digest, registryDigest)
			}

			const warning = "digest file was empty or invalid"
			if warned := strings.Contains(log.String(), warning); warned != testCase.wantWarn {
				t.Errorf("warned = %v, want %v; log = %q", warned, testCase.wantWarn, log.String())
			}
		})
	}
}

// TestPushImage_InputRefusals covers validatePushImageInput, which had
// about half its branches exercised. Nothing may be pushed on a refusal.
//
// The tls-verify check is the one that carries weight: the field is a
// string, and an unrecognised value silently treated as "false" would
// push a release image over an unverified TLS connection. It is refused
// instead, so a typo fails the run rather than downgrading it.
func TestPushImage_InputRefusals(t *testing.T) {
	t.Parallel()

	valid := func() appcontainer.PushImageInput {
		return appcontainer.PushImageInput{
			LocalImage:  "localhost/app:arch",
			Destination: "registry.example/app:staging-amd64",
			TLSVerify:   "true",
		}
	}

	t.Run("missing collaborators", func(t *testing.T) {
		t.Parallel()

		// Unchecked, a nil adapter is a panic rather than an error.
		if _, err := appcontainer.PushImage(context.Background(), nil, &fakeManifestPushRegistry{}, fakeoutputsink.New(t), io.Discard, valid()); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("nil push tool: err = %v, want ErrUsage", err)
		}

		if _, err := appcontainer.PushImage(context.Background(), &fakeImagePushTool{}, nil, fakeoutputsink.New(t), io.Discard, valid()); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("nil registry: err = %v, want ErrUsage", err)
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*appcontainer.PushImageInput)
	}{
		{name: "no local image", mutate: func(in *appcontainer.PushImageInput) { in.LocalImage = "" }},
		{name: "blank local image", mutate: func(in *appcontainer.PushImageInput) { in.LocalImage = "   " }},
		{name: "no destination", mutate: func(in *appcontainer.PushImageInput) { in.Destination = "" }},

		// Whitespace in a ref is refused because the ref reaches an argv
		// entry; a space would split it into two arguments.
		{name: "destination with a space", mutate: func(in *appcontainer.PushImageInput) { in.Destination = "registry.example/app:tag --tls-verify=false" }},
		{name: "destination with a newline", mutate: func(in *appcontainer.PushImageInput) { in.Destination = "registry.example/app:tag\nevil" }},
		{name: "destination with a tab", mutate: func(in *appcontainer.PushImageInput) { in.Destination = "registry.example/app:tag\tevil" }},

		{name: "tls-verify unset", mutate: func(in *appcontainer.PushImageInput) { in.TLSVerify = "" }},
		{name: "tls-verify yes", mutate: func(in *appcontainer.PushImageInput) { in.TLSVerify = "yes" }},
		{name: "tls-verify TRUE", mutate: func(in *appcontainer.PushImageInput) { in.TLSVerify = "TRUE" }},
		{name: "tls-verify 1", mutate: func(in *appcontainer.PushImageInput) { in.TLSVerify = "1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := valid()
			tc.mutate(&in)

			tool := &fakeImagePushTool{}
			registry := &fakeManifestPushRegistry{}

			_, err := appcontainer.PushImage(context.Background(), tool, registry, fakeoutputsink.New(t), io.Discard, in)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}

			if len(tool.pushes) != 0 {
				t.Errorf("pushed %v on a refused input", tool.pushes)
			}
		})
	}
}

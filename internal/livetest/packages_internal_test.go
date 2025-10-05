// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func packageCleanupRequest(t *testing.T, forge provider.ForgeAPI, mode string) (packageRequest, *[]string) {
	t.Helper()

	var calls []string

	reads := 0
	request := func(method, endpoint string, into any, accept ...int) error {
		calls = append(calls, method)
		if method == http.MethodDelete {
			want := "https://packages.invalid/api/v1/packages/fixture/npm/rc-package/1.2.3"
			if forge == provider.ForgeGitLab {
				want = "https://packages.invalid/api/v4/projects/fixture%2Frc-repo/packages/7"
			}

			if endpoint != want {
				t.Errorf("delete endpoint=%s", endpoint)
			}

			if !reflect.DeepEqual(accept, []int{204, 200, 404}) {
				t.Errorf("delete statuses=%v", accept)
			}

			if mode == "delete error" {
				return errs.ErrPermissionDenied
			}

			return nil
		}

		reads++
		if mode == "lookup error" || (mode == "verify error" && reads == 2) {
			return errs.ErrPermissionDenied
		}

		body := `[]`
		if mode != "absent" && (reads == 1 || mode == "still present") {
			body = `[{"id":7,"name":"rc-package","version":"1.2.3","type":"npm","package_type":"npm"},{"id":8,"name":"rc-package","version":"9.9.9","type":"npm","package_type":"npm"}]`
		}

		return json.Unmarshal([]byte(body), into)
	}

	return request, &calls
}

func TestPackageCleanup_RequiresSuccessfulLookupDeletionAndAbsence(t *testing.T) {
	t.Parallel()

	for _, forge := range []provider.ForgeAPI{provider.ForgeForgejo, provider.ForgeGitLab} {
		for _, tc := range []struct {
			mode, calls string
			want        error
		}{
			{"removed", "GET DELETE GET", nil}, {"absent", "GET GET", nil},
			{"lookup error", "GET", errs.ErrPermissionDenied}, {"delete error", "GET DELETE", errs.ErrPermissionDenied},
			{"verify error", "GET DELETE GET", errs.ErrPermissionDenied}, {"still present", "GET DELETE GET", errs.ErrValidation},
		} {
			t.Run(string(forge)+"/"+tc.mode, func(t *testing.T) {
				t.Parallel()

				target := Target{Forge: forge, Host: "packages.invalid", Owner: "fixture"}
				request, calls := packageCleanupRequest(t, forge, tc.mode)

				err := deletePublishedPackage(target, "rc-repo", "npm", "rc-package", "1.2.3", request)
				if !errors.Is(err, tc.want) {
					t.Errorf("err=%v, want=%v", err, tc.want)
				}

				if strings.Join(*calls, " ") != tc.calls {
					t.Errorf("calls=%v, want=%s", *calls, tc.calls)
				}
			})
		}
	}
}

func TestPackageCleanup_LookupChecksLaterPages(t *testing.T) {
	t.Parallel()

	for _, forge := range []provider.ForgeAPI{provider.ForgeForgejo, provider.ForgeGitLab} {
		target := Target{Forge: forge, Host: "packages.invalid", Owner: "fixture"}
		reads := 0
		request := func(method, endpoint string, into any, _ ...int) error {
			reads++

			parsed, err := url.Parse(endpoint)
			if err != nil {
				return err
			}

			if method != http.MethodGet {
				t.Fatalf("unexpected mutation: %s", method)
			}

			if parsed.Query().Get("page") == "1" {
				return json.Unmarshal([]byte("["+strings.Repeat(`{"name":"other"},`, 99)+`{"name":"other"}]`), into)
			}

			if parsed.Query().Get("page") != "2" {
				t.Fatal("unexpected page")
			}

			return json.Unmarshal([]byte(`[{"id":7,"name":"rc-package","version":"1.2.3","type":"npm","package_type":"npm"}]`), into)
		}

		packages, err := publishedPackages(target, "rc-repo", "npm", "rc-package", request)
		if err != nil || reads != 2 || len(packages) != 1 || packages[0].Version != "1.2.3" {
			t.Fatalf("lookup=%v reads=%d err=%v", packages, reads, err)
		}
	}
}

func TestPackageCleanup_TrustFailureFailsTheTest(t *testing.T) {
	t.Parallel()
	// An owned directory is not a CA file, so transport construction fails
	// before any request. No live target or credential is involved.
	recorder := &fatalRecorder{}
	DeletePublishedPackage(recorder, Target{accepted: true, Forge: provider.ForgeGitLab, Host: "packages.invalid", Owner: "fixture", CAFile: t.TempDir()}, "rc-repo", "npm", "rc-package", "1.2.3")

	if !recorder.failed || recorder.fatal {
		t.Fatal("cleanup error must fail the test without aborting other cleanups")
	}
}

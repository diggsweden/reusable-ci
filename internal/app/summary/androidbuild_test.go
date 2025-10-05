// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

func TestAndroidBuild_ForwardsAllFields(t *testing.T) {
	t.Parallel()

	const (
		heading = "## Android Variants Build Summary 📱\n\n### Configuration\n| Setting | Value |\n|---------|-------|\n"
		footer  = "\n*Build completed at 2026-05-10 14:30:00 UTC*\n"
	)
	// Every pair of flags takes all four combinations, so forwarding one
	// flag (or its inverse) in place of another cannot satisfy the table.
	for _, tc := range []struct {
		name string
		in   appsummary.AndroidBuildInput
		want string
	}{
		{
			name: "signed_bundle_with_tests",
			in: appsummary.AndroidBuildInput{
				JavaVersion: "25", JDKDist: "Temurin", BuildModule: "mobile", Flavor: "fdroid",
				BuildTypes: "debug,release", IncludeAAB: true, Signing: true, SkipTests: false,
				Version: "2.4.6", VersionCode: "246", DebugName: "phone-debug", ReleaseName: "phone-release", AABName: "phone-bundle",
			},
			want: `| **Java** | 25 (Temurin) |
| **Module** | mobile |
| **Flavor** | fdroid |
| **Build Types** | debug,release |
| **Include AAB** | ✓ |
| **Signing** | ✓ Enabled |
| **Tests** | ✓ Executed |
| **Version** | 2.4.6 (246) |

### Artifacts Generated
✓ Debug APK: ` + "`phone-debug`\n✓ Release APK: `phone-release`\n✓ Release AAB: `phone-bundle`\n",
		},
		{
			name: "unsigned_bundle_without_tests",
			in: appsummary.AndroidBuildInput{
				JavaVersion: "21", JDKDist: "Zulu", BuildModule: "tablet", Flavor: "store",
				BuildTypes: "release", IncludeAAB: true, Signing: false, SkipTests: true,
				Version: "3.5.7", VersionCode: "357", DebugName: "unused-debug", ReleaseName: "tablet-release", AABName: "tablet-bundle",
			},
			want: `| **Java** | 21 (Zulu) |
| **Module** | tablet |
| **Flavor** | store |
| **Build Types** | release |
| **Include AAB** | ✓ |
| **Signing** | ⊘ Disabled |
| **Tests** | ⊘ Skipped |
| **Version** | 3.5.7 (357) |

### Artifacts Generated
✓ Release APK: ` + "`tablet-release`\n✓ Release AAB: `tablet-bundle`\n",
		},
		{
			name: "signed_apk_without_bundle_or_tests",
			in: appsummary.AndroidBuildInput{
				JavaVersion: "17", JDKDist: "Corretto", BuildModule: "watch", Flavor: "demo",
				BuildTypes: "release", IncludeAAB: false, Signing: true, SkipTests: true,
				Version: "4.6.8", VersionCode: "468", DebugName: "unused-debug", ReleaseName: "watch-release", AABName: "unused-bundle",
			},
			want: `| **Java** | 17 (Corretto) |
| **Module** | watch |
| **Flavor** | demo |
| **Build Types** | release |
| **Include AAB** | ✗ |
| **Signing** | ✓ Enabled |
| **Tests** | ⊘ Skipped |
| **Version** | 4.6.8 (468) |

### Artifacts Generated
✓ Release APK: ` + "`watch-release`\n",
		},
		{
			name: "unsigned_debug_with_tests_and_default_flavor",
			in: appsummary.AndroidBuildInput{
				JavaVersion: "24", JDKDist: "Liberica", BuildModule: "reader", Flavor: "",
				BuildTypes: "debug", IncludeAAB: false, Signing: false, SkipTests: false,
				Version: "5.7.9", VersionCode: "579", DebugName: "reader-debug", ReleaseName: "unused-release", AABName: "unused-bundle",
			},
			want: `| **Java** | 24 (Liberica) |
| **Module** | reader |
| **Flavor** | default |
| **Build Types** | debug |
| **Include AAB** | ✗ |
| **Signing** | ⊘ Disabled |
| **Tests** | ✓ Executed |
| **Version** | 5.7.9 (579) |

### Artifacts Generated
✓ Debug APK: ` + "`reader-debug`\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}

			tc.in.Now = fixedNow()
			if err := appsummary.AndroidBuild(t.Context(), sink, tc.in); err != nil {
				t.Fatal(err)
			}

			if got, want := sink.buf.String(), heading+tc.want+footer; got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
		})
	}
}

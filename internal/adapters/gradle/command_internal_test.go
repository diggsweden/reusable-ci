// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gradle

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommand_BindsDirectoryEnvironmentArgsAndWriters(t *testing.T) {
	for _, binary := range []string{"", "/owned/custom gradlew"} {
		for _, signed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/signed_%t", binary, signed), func(t *testing.T) {
				t.Setenv("JAVA_HOME", "/owned/jdk")
				t.Setenv("ANDROID_HOME", "/owned/sdk")
				t.Setenv("ANDROID_SDK_ROOT", "/owned/sdk-root")
				t.Setenv("GRADLE_USER_HOME", "/owned/gradle-home")
				t.Setenv("UNRELATED_RUNTIME", "retained-not-a-sandbox")

				for _, key := range []string{"ANDROID_KEYSTORE_PATH", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
					t.Setenv(key, "ambient-"+key)
				}

				before := os.Environ()

				var overrides []string
				if signed {
					overrides = []string{"ANDROID_KEYSTORE_PATH=/owned/private/release.keystore", "ANDROID_KEYSTORE_PASSWORD= store\n", "ANDROID_KEY_ALIAS= alias ", "ANDROID_KEY_PASSWORD=\tkey\r\n"}
				}

				var stdout, stderr bytes.Buffer

				adapter := &Adapter{Bin: binary}
				args := []string{"--init-script", "/owned/init file.gradle", "cyclonedxBom"}
				cmd := adapter.command(t.Context(), "/owned/selected project", overrides, &stdout, &stderr, args...)

				wantBin := binary
				if wantBin == "" {
					wantBin = "./gradlew"
				}

				require.Equal(t, wantBin, cmd.Path)
				require.Equal(t, append([]string{wantBin}, args...), cmd.Args)
				require.Equal(t, "/owned/selected project", cmd.Dir)
				require.Same(t, &stdout, cmd.Stdout)
				require.Same(t, &stderr, cmd.Stderr)

				wantEnv := make(map[string]string)

				for _, entry := range append(before, overrides...) {
					key, value, ok := strings.Cut(entry, "=")
					require.True(t, ok)

					wantEnv[key] = value
				}

				wantEnv["PWD"] = "/owned/selected project"
				actual := make(map[string]string)

				for _, entry := range cmd.Env {
					key, value, ok := strings.Cut(entry, "=")
					require.True(t, ok)
					require.NotContains(t, actual, key, "duplicate effective environment key")
					actual[key] = value
				}

				require.Equal(t, wantEnv, actual)
				require.Equal(t, before, os.Environ())
				require.Zero(t, stdout.Len()+stderr.Len())
				require.Nil(t, cmd.Process, "builder must not start a process")

				if signed {
					require.NotContains(t, strings.Join(cmd.Args, " "), "store")
					require.NotContains(t, strings.Join(cmd.Args, " "), "alias")
					require.NotContains(t, strings.Join(cmd.Args, " "), "key")
				}
			})
		}
	}
}

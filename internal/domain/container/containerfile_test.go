// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
)

func TestContainerfileRebuildsFromSource_KnownPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  bool
	}{
		{"mvn_package", "FROM eclipse-temurin\nRUN mvn package -DskipTests", true},
		{"mvn_install", "RUN mvn install", true},
		{"mvnw_package", "RUN ./mvnw package", true},
		{"gradle_build", "RUN gradle build", true},
		{"gradle_assemble", "RUN gradle assemble", true},
		{"npm_run_build", "RUN npm run build", true},
		{"go_build", "RUN go build -o /out/demo ./cmd/demo", true},
		{"plain_copy_only_build", "FROM alpine\nCOPY app /app\nCMD [\"/app\"]", false},
		{"empty", "", false},
		{"comment_with_pattern_still_flagged", "# mvn install will rebuild", true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t,
				testCase.want,
				container.ContainerfileRebuildsFromSource(testCase.given),
			)
		})
	}
}

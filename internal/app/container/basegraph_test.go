// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestBaseGraphInputsMatchShellHashContract(t *testing.T) {
	t.Parallel()

	root := writeBaseGraphFixture(t)

	result, err := appcontainer.BaseGraphInputs(appcontainer.BaseGraphInput{Root: root, ArchSet: []string{"amd64,arm64"}})
	if err != nil {
		t.Fatal(err)
	}

	commonDigest := baseGraphTestFileDigest("common.txt", "common\n")
	argDigest := baseGraphTestFragmentDigest("containerfile:arg:BASE", "ARG BASE=debian\n")
	coreFileDigest := baseGraphTestFileDigest("core.txt", "core\n")
	coreStageDigest := baseGraphTestFragmentDigest("containerfile:stage:core", "FROM scratch AS core\nCOPY core /core\n")
	coreContentID := baseGraphTestSHA("base=core\nkind=content\n" + commonDigest + argDigest + coreFileDigest + coreStageDigest)
	coreBaseInputID := baseGraphTestSHA(fmt.Sprintf("base=core\nkind=manifest\ncontent_id=%s\narch_set=amd64,arm64\n", coreContentID))

	fullFileDigest := baseGraphTestFileDigest("full.txt", "full\n")
	fullStageDigest := baseGraphTestFragmentDigest("containerfile:stage:full", "FROM scratch AS full\nCOPY full /full\n")
	fullContentID := baseGraphTestSHA(fmt.Sprintf("base=full\nkind=content\nparent:core=%s\n", coreContentID) + commonDigest + argDigest + fullFileDigest + fullStageDigest)
	fullBaseInputID := baseGraphTestSHA(fmt.Sprintf("base=full\nkind=manifest\ncontent_id=%s\narch_set=amd64,arm64\n", fullContentID))

	wantJSONBytes, err := json.Marshal([]appcontainer.BaseInput{
		{Flavor: "core", ContentID: coreContentID, BaseInputID: coreBaseInputID},
		{Flavor: "full", ContentID: fullContentID, BaseInputID: fullBaseInputID},
	})
	if err != nil {
		t.Fatal(err)
	}

	wantJSON := string(wantJSONBytes)

	if result.InputsJSON != wantJSON {
		t.Fatalf("inputs JSON = %s, want %s", result.InputsJSON, wantJSON)
	}

	if result.InputSetID != baseGraphTestSHA(wantJSON+"\n") {
		t.Fatalf("input set ID = %s", result.InputSetID)
	}

	single, err := appcontainer.BaseGraphSingleInput(appcontainer.BaseGraphInput{Root: root, ArchSet: []string{"amd64", "arm64"}}, "full")
	if err != nil {
		t.Fatal(err)
	}

	if single.ContentID != fullContentID || single.BaseInputID != fullBaseInputID {
		t.Fatalf("single input = %+v", single)
	}
}

func TestBaseGraphGroups(t *testing.T) {
	t.Parallel()

	root := writeBaseGraphFixture(t)

	groupsJSON, err := appcontainer.BaseGraphGroups(root, "")
	if err != nil {
		t.Fatal(err)
	}

	wantGroupsJSON := `[{"group":"core-ish","flavors":["core"],"context_files":["ctx/core.toml"]},{"group":"all","flavors":["core","full"],"context_files":[]}]`
	if groupsJSON != wantGroupsJSON {
		t.Fatalf("groups JSON = %s, want %s", groupsJSON, wantGroupsJSON)
	}

	flavors, err := appcontainer.BaseGraphGroupFlavors(appcontainer.BaseGraphGroupInput{Root: root, Group: "all"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(flavors, []string{"core", "full"}) {
		t.Fatalf("flavors = %#v", flavors)
	}

	files, err := appcontainer.BaseGraphGroupContextFiles(appcontainer.BaseGraphGroupInput{Root: root, Group: "core-ish"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(files, []string{"ctx/core.toml"}) {
		t.Fatalf("context files = %#v", files)
	}

	missingFull, err := appcontainer.BaseGraphGroupsForMissing(appcontainer.BaseGraphGroupsForMissingInput{Root: root, MissingJSON: `["full"]`})
	if err != nil {
		t.Fatal(err)
	}

	if missingFull != `["all"]` {
		t.Fatalf("groups for full = %s", missingFull)
	}

	missingCore, err := appcontainer.BaseGraphGroupsForMissing(appcontainer.BaseGraphGroupsForMissingInput{GroupsJSON: groupsJSON, MissingFlavor: []string{"core"}})
	if err != nil {
		t.Fatal(err)
	}

	if missingCore != `["core-ish","all"]` {
		t.Fatalf("groups for core = %s", missingCore)
	}
}

func TestBaseGraphRejectsUnknownFlavor(t *testing.T) {
	t.Parallel()

	_, err := appcontainer.BaseGraphSingleInput(appcontainer.BaseGraphInput{Root: writeBaseGraphFixture(t)}, "missing")
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "unknown flavor") {
		t.Fatalf("err = %v, want unknown flavor validation", err)
	}
}

func writeBaseGraphFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	baseGraphTestWrite(t, root, "packaging/container/base-graph.json", `{
  "common_inputs": ["common.txt", "containerfile:arg:BASE"],
  "flavors": [
    {"flavor": "core", "parents": [], "inputs": ["core.txt", "containerfile:stage:core"]},
    {"flavor": "full", "parents": ["core"], "inputs": ["full.txt", "containerfile:stage:full"]}
  ],
  "groups": [
    {"group": "core-ish", "flavors": ["core"], "context_files": ["ctx/core.toml"]},
    {"group": "all", "flavors": ["core", "full"], "context_files": []}
  ]
}`)
	baseGraphTestWrite(t, root, "packaging/container/Containerfile", "ARG BASE=debian\nFROM scratch AS core\nCOPY core /core\nFROM scratch AS full\nCOPY full /full\n")
	baseGraphTestWrite(t, root, "common.txt", "common\n")
	baseGraphTestWrite(t, root, "core.txt", "core\n")
	baseGraphTestWrite(t, root, "full.txt", "full\n")

	return root
}

func baseGraphTestWrite(t *testing.T, root, name, body string) {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func baseGraphTestFileDigest(path, body string) string {
	return fmt.Sprintf("path=%s\n%s  %s\n", path, baseGraphTestSHA(body), path)
}

func baseGraphTestFragmentDigest(fragment, body string) string {
	return fmt.Sprintf("fragment=%s\nsha256=%s\n", fragment, baseGraphTestSHA(body))
}

func baseGraphTestSHA(body string) string {
	sum := sha256.Sum256([]byte(body))

	return hex.EncodeToString(sum[:])
}

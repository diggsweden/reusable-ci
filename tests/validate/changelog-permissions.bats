#!/usr/bin/env bats
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"

@test "all changelog call paths grant PR read access without a release PAT" {
  run python3 - "${BATS_TEST_DIRNAME}/../.." <<'PY'
import copy
import json
import subprocess
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
leaf = ".github/workflows/generate-changelog.yml"
prefix = "diggsweden/reusable-ci/"


def load(path):
    return json.loads(subprocess.check_output(["yq", "-o=json", ".", str(path)], text=True))


workflows = {str(p.relative_to(root)): load(p) for p in (root / ".github/workflows").glob("*.yml")}
examples = {str(p.relative_to(root)): load(p) for p in (root / "examples").glob("*/*workflow.yml")}


def target(job):
    uses = job.get("uses", "")
    if uses.startswith("./.github/workflows/"):
        return uses[2:]
    if uses.startswith(prefix):
        return uses[len(prefix):].split("@", 1)[0]
    return None


def reaches_changelog(name):
    if name == leaf:
        return True
    return any(reaches_changelog(target(job)) for job in workflows.get(name, {}).get("jobs", {}).values())


def permission(doc, job, key):
    permissions = job.get("permissions", doc.get("permissions", {}))
    if isinstance(permissions, str):
        return {"read-all": "read", "write-all": "write"}.get(permissions)
    return permissions.get(key)


def check_callers(documents):
    return [f"{name}:{job_id}" for name, doc in documents.items()
            for job_id, job in doc.get("jobs", {}).items()
            if reaches_changelog(target(job)) and permission(doc, job, "pull-requests") not in ("read", "write")]


documents = {**workflows, **examples}
assert not check_callers(documents), check_callers(documents)
generate = workflows[leaf]
job = generate["jobs"]["generate-changelog"]
assert job["permissions"] == {"contents": "read", "pull-requests": "read"}
assert not generate["on"]["workflow_call"].get("secrets"), "Changelog must not require a PAT"
for env in [generate.get("env", {}), job.get("env", {})]:
    assert "GITHUB_TOKEN" not in env, "Use the automatic token"
cliff = next(step for step in job["steps"] if step.get("uses", "").startswith("orhun/git-cliff-action@"))
assert "GITHUB_TOKEN" not in cliff.get("env", {})
assert cliff.get("with", {}).get("github_token", "${{ github.token }}") == "${{ github.token }}"

# A permission dropped at any intermediate boundary must be detected, even
# when the leaf and the consumer both request it correctly.
edges = [(name, job_id) for name, doc in documents.items()
         for job_id, caller in doc.get("jobs", {}).items() if reaches_changelog(target(caller))]
assert edges, "No changelog callers discovered"
for name, job_id in edges:
    broken = copy.deepcopy(documents)
    broken[name]["jobs"][job_id]["permissions"] = {"contents": "read"}
    assert f"{name}:{job_id}" in check_callers(broken)

print(f"Validated {len(edges)} changelog call boundaries, read-only leaf permissions, and automatic-token use.")
PY
  assert_success
}

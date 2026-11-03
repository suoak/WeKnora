"""Read-only, fail-closed provenance gate for paired production images.

Input files are private `docker image inspect` snapshots, not container env
exports. Only source labels, image IDs and registry digests enter the receipt.
The caller must separately verify image bytes and run application acceptance.
"""

import argparse
import datetime
import json
from pathlib import Path
import re
import subprocess


def git(repo, *args):
    return subprocess.check_output(
        ["git", "-C", str(repo), *args], text=True, encoding="utf-8"
    ).strip()


def image_identity(snapshot, expected_id):
    if isinstance(snapshot, list):
        if len(snapshot) != 1:
            raise ValueError("exactly one image required")
        snapshot = snapshot[0]
    image_id = snapshot.get("Id", "")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", expected_id) or image_id != expected_id:
        raise ValueError("image configuration digest mismatch")
    source = snapshot.get("Config", {}).get("Labels", {}).get(
        "org.opencontainers.image.revision", ""
    )
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("full image source SHA missing")
    return {
        "source_sha": source,
        "image_config_digest": image_id,
        "registry_digests": snapshot.get("RepoDigests") or [],
    }


def verify(repo, app, frontend, app_id, frontend_id, expected_main, remote="origin"):
    if not re.fullmatch(r"[0-9a-f]{40}", expected_main):
        raise ValueError("full expected main SHA required")
    # A stale origin/main tracking ref is insufficient for production release.
    line = git(repo, "ls-remote", "--exit-code", remote, "refs/heads/main")
    fields = line.split()
    if len(fields) != 2 or fields[0] != expected_main or fields[1] != "refs/heads/main":
        raise ValueError("remote main changed; regenerate the release receipt")
    identities = {
        "app": image_identity(app, app_id),
        "frontend": image_identity(frontend, frontend_id),
    }
    if identities["app"]["source_sha"] != identities["frontend"]["source_sha"]:
        raise ValueError("APP and Frontend must use the same validated source SHA")
    for identity in identities.values():
        source = identity["source_sha"]
        if git(repo, "rev-parse", "--verify", source + "^{commit}") != source:
            raise ValueError("image source commit is not available")
        result = subprocess.run(
            ["git", "-C", str(repo), "merge-base", "--is-ancestor", expected_main, source],
            capture_output=True,
        )
        if result.returncode != 0:
            raise ValueError("image omits current main history")
        identity["contains_main"] = True
    return {
        "version": 1,
        "checked_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "main_sha": expected_main,
        "paired_source_sha": identities["app"]["source_sha"],
        "images": identities,
        "authentication_exported": False,
        "production_changes": False,
        "image_bytes_and_business_acceptance_required_separately": True,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--app-inspect", type=Path, required=True)
    parser.add_argument("--frontend-inspect", type=Path, required=True)
    parser.add_argument("--expected-app-image-id", required=True)
    parser.add_argument("--expected-frontend-image-id", required=True)
    parser.add_argument("--expected-main-sha", required=True)
    parser.add_argument("--main-remote", default="origin")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    result = verify(
        args.repository,
        json.loads(args.app_inspect.read_text(encoding="utf-8")),
        json.loads(args.frontend_inspect.read_text(encoding="utf-8")),
        args.expected_app_image_id,
        args.expected_frontend_image_id,
        args.expected_main_sha,
        args.main_remote,
    )
    # An old successful receipt must not be silently replaced after a failure.
    with args.output.open("x", encoding="utf-8") as output:
        json.dump(result, output, indent=2)
        output.write("\n")
    print(json.dumps({"verified": True, "sha": result["paired_source_sha"]}))


if __name__ == "__main__":
    main()

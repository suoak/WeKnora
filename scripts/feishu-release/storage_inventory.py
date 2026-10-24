"""Bounded, read-only storage inventory. Never removes images, files or backups."""
import argparse
import json
import os
from pathlib import Path
import shutil
import time


def inventory(root, limit):
    root = Path(root).resolve(strict=True)
    rows = []
    pending = [root]
    errors = 0
    visited = 0
    while pending and visited < limit:
        directory = pending.pop()
        try:
            with os.scandir(directory) as entries:
                for entry in entries:
                    visited += 1
                    if visited > limit:
                        break
                    if entry.is_symlink():
                        continue
                    if entry.is_dir(follow_symlinks=False):
                        pending.append(Path(entry.path))
                    elif entry.is_file(follow_symlinks=False):
                        stat = entry.stat(follow_symlinks=False)
                        rows.append({"path": entry.path, "bytes": stat.st_size,
                                     "age_seconds": max(0, int(time.time()-stat.st_mtime)),
                                     "disposition": "retain_until_identity_reference_and_replica_verified"})
        except OSError:
            errors += 1
    return {"root": str(root), "free_bytes": shutil.disk_usage(root).free,
            "bounded_complete": not pending and visited <= limit and errors == 0,
            "errors": errors, "entries_examined": visited,
            "files": sorted(rows, key=lambda x: x["bytes"], reverse=True),
            "automatic_deletes": False}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", action="append", required=True)
    parser.add_argument("--max-entries", type=int, default=10000)
    args = parser.parse_args()
    if not 1 <= args.max_entries <= 100000:
        parser.error("max entries must be between 1 and 100000")
    print(json.dumps([inventory(root, args.max_entries) for root in args.root]))

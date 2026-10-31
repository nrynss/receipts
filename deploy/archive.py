#!/usr/bin/env python3
"""Validate and extract one Receipts SQLite backup without following links."""

import os
import shutil
import sqlite3
import sys
import tarfile
from pathlib import Path

MAX_UNCOMPRESSED_BYTES = 4 * 1024 * 1024 * 1024
ALLOWED_DB_FILES = {"receipts.db", "receipts.db-wal", "receipts.db-shm", "receipts.db-journal"}


def fail(message):
    print(f"archive: {message}", file=sys.stderr)
    raise SystemExit(1)


def extract(archive, base, staging):
    if not base or base in {".", ".."} or "/" in base or "\\" in base:
        fail("invalid data directory name")
    destination = Path(staging) / base
    total = 0
    files = {}
    try:
        with tarfile.open(archive, "r:gz") as bundle:
            members = []
            for member in bundle:
                members.append(member)
                if len(members) > 5:
                    fail("archive contains too many members")
                name = member.name.rstrip("/")
                if name == base:
                    if not member.isdir():
                        fail("archive root is not a directory")
                    continue
                prefix = base + "/"
                if not name.startswith(prefix):
                    fail("archive contains a path outside its data directory")
                leaf = name[len(prefix) :]
                if not leaf or "/" in leaf or leaf not in ALLOWED_DB_FILES:
                    fail("archive contains an unexpected path")
                if not member.isreg():
                    fail("database archive contains a link or special file")
                total += member.size
                if total > MAX_UNCOMPRESSED_BYTES:
                    fail("uncompressed archive exceeds 4 GiB")
                if leaf in files:
                    fail("archive contains duplicate database files")
                files[leaf] = member
            if "receipts.db" not in files:
                fail("archive does not contain receipts.db")

            os.mkdir(destination, 0o750)
            for leaf, member in files.items():
                source = bundle.extractfile(member)
                if source is None:
                    fail("could not read a database file")
                target = destination / leaf
                descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(descriptor, "wb") as output, source:
                    shutil.copyfileobj(source, output, length=1024 * 1024)
    except (OSError, tarfile.TarError) as exc:
        fail(str(exc))

    database = destination / "receipts.db"
    try:
        connection = sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)
        with connection:
            result = connection.execute("PRAGMA integrity_check").fetchone()
            if result is None or result[0] != "ok":
                fail("SQLite integrity_check failed")
            connection.execute("SELECT count(*) FROM receipts").fetchone()
        connection.close()
    except sqlite3.Error as exc:
        fail(f"SQLite validation failed: {exc}")


if __name__ == "__main__":
    if len(sys.argv) != 5 or sys.argv[1] != "extract":
        fail("usage: archive.py extract archive.tar.gz directory staging-dir")
    extract(sys.argv[2], sys.argv[3], sys.argv[4])

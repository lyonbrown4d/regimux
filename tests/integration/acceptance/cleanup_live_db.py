"""Postgres snapshot and aging helpers for live cleanup acceptance."""
from __future__ import annotations

import os
import time
from datetime import datetime, timezone
from typing import Any

import psycopg
from psycopg import sql
from botocore.exceptions import ClientError
from psycopg.rows import DictRow, dict_row
from mypy_boto3_s3 import S3Client

from cleanup_live_support import Case, OLD_NS, require_env

Query = sql.SQL | sql.Composed


def pg_connect() -> psycopg.Connection[DictRow]:
    return psycopg.Connection[DictRow].connect(require_env("PG_DSN"), row_factory=dict_row)


def selected_tags(conn: psycopg.Connection[DictRow], cases: list[Case]) -> list[DictRow]:
    where, args = identity_predicate(cases)
    deadline = time.monotonic() + 60
    while True:
        with conn.cursor() as cur:
            cur.execute(sql.SQL("SELECT * FROM meta_tags WHERE {}").format(where), args)
            rows = list(cur.fetchall())
        found = {(r["alias"], r["repository"], r["reference"]) for r in rows}
        missing = [c.id for c in cases if (c.alias, c.repository, c.reference) not in found]
        if not missing:
            return rows
        if time.monotonic() >= deadline:
            raise RuntimeError(f"real cache warm did not publish expected tag rows within 60s: {missing}")
        time.sleep(0.5)


def identity_predicate(cases: list[Case], prefix: str = "") -> tuple[Query, list[str]]:
    def column(name: str) -> sql.Identifier:
        return sql.Identifier(prefix, name) if prefix else sql.Identifier(name)

    clauses = [sql.SQL("({}=%s AND {}=%s AND {}=%s)").format(column("alias"), column("repository"), column("reference")) for _ in cases]
    args = [value for c in cases for value in (c.alias, c.repository, c.reference)]
    return sql.SQL(" OR ").join(clauses) if clauses else sql.SQL("FALSE"), args


def pair_predicate(pairs: set[tuple[str, str]], prefix: str = "") -> tuple[Query, list[str]]:
    def column(name: str) -> sql.Identifier:
        return sql.Identifier(prefix, name) if prefix else sql.Identifier(name)

    clauses = [sql.SQL("({}=%s AND {}=%s)").format(column("alias"), column("repository")) for _ in sorted(pairs)]
    args = [part for pair in sorted(pairs) for part in pair]
    return sql.SQL(" OR ").join(clauses) if clauses else sql.SQL("FALSE"), args


def pull_predicate(cases: list[Case]) -> tuple[Query, list[str]]:
    identities = {(c.alias, c.repository, c.reference) for c in cases if c.ecosystem == "container"}
    identities.update((alias, c.repository, c.reference) for c in cases if c.ecosystem != "container" for alias in (c.alias, f"{c.ecosystem}/{c.alias}"))
    clauses = [sql.SQL("(alias=%s AND repository=%s AND reference=%s)") for _ in sorted(identities)]
    args = [value for row in sorted(identities) for value in row]
    return sql.SQL(" OR ").join(clauses) if clauses else sql.SQL("FALSE"), args


def broad_pairs(cases: list[Case]) -> set[tuple[str, str]]:
    return {(case.alias, case.repository) for case in cases if case.ecosystem in {"go", "container"}}


def cleanup_metadata_expectations(
    before: dict[str, Any],
    cases: list[Case],
    expired: set[tuple[str, str, str]],
) -> tuple[
    set[tuple[str, ...]], set[tuple[str, ...]], set[tuple[str, ...]],
    set[tuple[str, ...]], set[tuple[str, ...]],
]:
    container_pairs = {(c.alias, c.repository) for c in cases if c.ecosystem == "container" and c.retention == "expire"}
    tags = {(r["alias"], r["repository"], r["reference"]): r for r in [*before["tags"], *before["related_tags"]]}
    owned_sources = {
        (r["alias"], r["repository"], r["digest"])
        for identity, r in tags.items()
        if identity in expired or (r["alias"], r["repository"]) in container_pairs
    }
    retained_sources = {
        (r["alias"], r["repository"], r["digest"])
        for identity, r in tags.items()
        if identity not in expired and (r["alias"], r["repository"]) not in container_pairs
    }
    retired_manifests = {
        (r["alias"], r["repository"], r["digest"])
        for r in before["manifests"]
        if ((r["alias"], r["repository"]) in container_pairs or (r["alias"], r["repository"], r["digest"]) in owned_sources)
        and (r["alias"], r["repository"], r["digest"]) not in retained_sources
    }
    retained_manifests = {
        (r["alias"], r["repository"], r["digest"])
        for r in before["manifests"]
        if (r["alias"], r["repository"], r["digest"]) in retained_sources
    }
    retained_link_keys = retained_sources
    retired_links = {
        (r["alias"], r["repository"], r["digest"])
        for r in before["repo_blobs"]
        if ((r["alias"], r["repository"]) in container_pairs or (r["alias"], r["repository"], r["source_manifest"]) in owned_sources)
        and (r["alias"], r["repository"], r["digest"]) not in retained_link_keys
    }
    retained_links = {
        (r["alias"], r["repository"], r["digest"])
        for r in before["repo_blobs"]
        if (r["alias"], r["repository"], r["digest"]) in retained_link_keys
    }
    keep_pull_identities = {
        (alias, c.repository, c.reference)
        for c in cases if c.retention == "keep"
        for alias in ((c.alias,) if c.ecosystem == "container" else (c.alias, f"{c.ecosystem}/{c.alias}"))
    }
    retained_pulls = {
        (r["alias"], r["repository"], r["reference"])
        for r in before["pulls"]
        if (r["alias"], r["repository"], r["reference"]) in keep_pull_identities
    }
    return retired_manifests, retired_links, retained_manifests, retained_links, retained_pulls


def natural_keys(
    conn: psycopg.Connection[DictRow],
    table: str,
    columns: tuple[str, ...],
    expected: set[tuple[str, ...]],
) -> set[tuple[str, ...]]:
    if not expected:
        return set()
    row_predicates: list[sql.Composed] = []
    params: list[str] = []
    for key in sorted(expected):
        row_predicates.append(sql.SQL("({})").format(sql.SQL(" AND ").join(
            sql.SQL("{}=%s").format(sql.Identifier(column)) for column in columns
        )))
        params.extend(key)
    query = sql.SQL("SELECT {} FROM {} WHERE {}").format(
        sql.SQL(",").join(sql.Identifier(column) for column in columns),
        sql.Identifier(table),
        sql.SQL(" OR ").join(row_predicates),
    )
    with conn.cursor() as cur:
        cur.execute(query, params)
        return {tuple(str(row[column]) for column in columns) for row in cur.fetchall()}


def snapshot(conn: psycopg.Connection[DictRow], cases: list[Case], tags: list[DictRow], s3: S3Client) -> dict[str, Any]:
    extra_tags = tags_for_pairs(conn, broad_pairs(cases))
    all_tags = {(r["alias"], r["repository"], r["reference"]): r for r in [*tags, *extra_tags]}
    tags = list(all_tags.values())
    repos = repo_blobs_for_cases(conn, cases)
    target_digests = {row["digest"] for row in tags} | {row["digest"] for row in repos}
    container_pairs = {(c.alias, c.repository) for c in cases if c.ecosystem == "container"}
    container_where, container_args = pair_predicate(container_pairs)
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SELECT digest FROM meta_manifests WHERE {}").format(container_where), container_args)
        target_digests.update(row["digest"] for row in cur.fetchall())
    digests = sorted(target_digests)
    where, args = identity_predicate(cases)
    container_manifest_where, container_manifest_args = pair_predicate(container_pairs, "m")
    pull_where, pull_args = pull_predicate(cases)
    pull_broad = {(alias, c.repository) for c in cases if c.ecosystem in {"go", "container"} for alias in ((c.alias, f"{c.ecosystem}/{c.alias}") if c.ecosystem != "container" else (c.alias,))}
    pull_broad_where, pull_broad_args = pair_predicate(pull_broad)
    state: dict[str, Any] = {"created_at": datetime.now(timezone.utc).isoformat(), "tags": tags}
    container_repo_where, container_repo_args = pair_predicate(container_pairs)
    queries: dict[str, tuple[Query, list[Any]]] = {
        "manifests": (sql.SQL("SELECT * FROM meta_manifests AS m WHERE m.digest = ANY(%s) OR {} OR {}").format(where, container_manifest_where), [digests, *args, *container_manifest_args]),
        "related_tags": (sql.SQL("SELECT * FROM meta_tags WHERE digest = ANY(%s)"), [digests]),
        "pulls": (sql.SQL("SELECT * FROM meta_pulls WHERE ({}) OR ({})").format(pull_where, pull_broad_where), [*pull_args, *pull_broad_args]),
        "repo_blobs": (sql.SQL("SELECT * FROM meta_repo_blobs WHERE digest = ANY(%s) OR {}").format(container_repo_where), [digests, *container_repo_args]),
        "blobs": (sql.SQL("SELECT * FROM meta_blobs WHERE digest = ANY(%s)"), [digests]),
    }
    records: dict[str, list[DictRow]] = {}
    with conn.cursor() as cur:
        for name, (query, params) in queries.items():
            cur.execute(query, params)
            records[name] = list(cur.fetchall())
            state[name] = records[name]
    keys = {row["digest"]: row["object_key"] for row in records["blobs"]}
    target_digests = set(digests)
    state["objects"] = {digest: head_object(s3, keys.get(digest, digest)) for digest in target_digests}
    missing = [digest for digest, info in state["objects"].items() if info is None]
    if missing:
        raise RuntimeError(f"cache metadata refers to missing S3 objects before cleanup: {missing}")
    return state


def head_object(s3: S3Client, object_key: str) -> dict[str, Any] | None:
    key = object_key.strip("/")
    prefix = os.getenv("S3_PREFIX", "cache").strip("/")
    if key.startswith(prefix + "/"):
        pass
    elif key.startswith("blobs/"):
        key = f"{prefix}/{key}" if prefix else key
    elif ":" in key:
        algorithm, encoded = key.split(":", 1)
        key = "/".join(part for part in (prefix, "blobs", algorithm, encoded[:2], encoded) if part)
    elif prefix:
        key = f"{prefix}/{key}"
    try:
        result = s3.head_object(Bucket=require_env("S3_BUCKET"), Key=key)
    except ClientError as exc:
        if exc.response.get("Error", {}).get("Code") in {"404", "NoSuchKey", "NotFound"}:
            return None
        raise
    return {"key": key, "size": result["ContentLength"], "etag": result.get("ETag"), "last_modified": result["LastModified"].isoformat()}


def age_test_records(conn: psycopg.Connection[DictRow], cases: list[Case], tags: list[DictRow], repos: list[DictRow]) -> None:
    where, args = identity_predicate(cases)
    tags = [*tags, *tags_for_pairs(conn, broad_pairs(cases))]
    tags = list({(r["alias"], r["repository"], r["reference"]): r for r in tags}.values())
    digests_set = {row["digest"] for row in tags} | {row["digest"] for row in repos}
    container_where, container_args = pair_predicate({(c.alias, c.repository) for c in cases if c.ecosystem == "container"})
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SELECT digest FROM meta_manifests WHERE {}").format(container_where), container_args)
        digests_set.update(row["digest"] for row in cur.fetchall())
    digests = sorted(digests_set)
    broad = broad_pairs(cases)
    broad_where, broad_args = pair_predicate(broad)
    pull_where, pull_args = pull_predicate(cases)
    pull_broad = {(alias, c.repository) for c in cases if c.ecosystem in {"go", "container"} for alias in ((c.alias, f"{c.ecosystem}/{c.alias}") if c.ecosystem != "container" else (c.alias,))}
    pull_broad_where, pull_broad_args = pair_predicate(pull_broad)
    with conn.cursor() as cur:
        cur.execute(sql.SQL("UPDATE meta_tags SET created_at=%s, updated_at=%s WHERE {} OR {}").format(where, broad_where), [OLD_NS, OLD_NS, *args, *broad_args])
        cur.execute(sql.SQL("UPDATE meta_pulls SET last_pull_at=%s, last_upstream_pull_at=%s, created_at=%s, updated_at=%s WHERE ({}) OR ({})").format(pull_where, pull_broad_where), [OLD_NS] * 4 + pull_args + pull_broad_args)
        cur.execute(sql.SQL("UPDATE meta_manifests AS m SET created_at=%s, updated_at=%s FROM meta_tags AS t WHERE m.alias=t.alias AND m.repository=t.repository AND m.digest=t.digest AND (({}) OR ({}))").format(identity_predicate(cases, "t")[0], pair_predicate(broad, "t")[0]), [OLD_NS, OLD_NS, *args, *broad_args])
        cur.execute(sql.SQL("UPDATE meta_manifests SET created_at=%s, updated_at=%s WHERE {}").format(container_where), [OLD_NS, OLD_NS, *container_args])
        repo_where, repo_args = pair_predicate({(c.alias, c.repository) for c in cases if c.ecosystem == "container"}, "r")
        cur.execute(sql.SQL("UPDATE meta_repo_blobs AS r SET created_at=%s, updated_at=%s, last_access_at=%s, last_verified_at=%s WHERE digest = ANY(%s) OR {}").format(repo_where), [OLD_NS] * 4 + [digests, *repo_args])
        cur.execute("UPDATE meta_blobs SET created_at=%s, updated_at=%s, last_access_at=%s WHERE digest = ANY(%s)", [OLD_NS] * 3 + [digests])
    conn.commit()


def repo_blobs_for_cases(conn: psycopg.Connection[DictRow], cases: list[Case]) -> list[DictRow]:
    tags = selected_tags(conn, cases)
    tags.extend(tags_for_pairs(conn, broad_pairs(cases)))
    digests = sorted({row["digest"] for row in tags})
    container_where, container_args = pair_predicate({(c.alias, c.repository) for c in cases if c.ecosystem == "container"})
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SELECT * FROM meta_repo_blobs WHERE digest = ANY(%s) OR {}").format(container_where), [digests, *container_args])
        return list(cur.fetchall())


def tags_for_pairs(conn: psycopg.Connection[DictRow], pairs: set[tuple[str, str]]) -> list[DictRow]:
    where, args = pair_predicate(pairs)
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SELECT * FROM meta_tags WHERE {}").format(where), args)
        return list(cur.fetchall())

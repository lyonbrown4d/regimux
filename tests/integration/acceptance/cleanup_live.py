#!/usr/bin/env python3
"""Live, destructive acceptance for scheduled dependency-cache cleanup.

Requires a disposable acceptance database/bucket, boto3, and psycopg 3.
"""
from __future__ import annotations

import concurrent.futures
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any, cast

import psycopg
from botocore.config import Config
from botocore.session import get_session
from mypy_boto3_s3 import S3Client
from psycopg import sql
from psycopg.rows import DictRow

from cleanup_live_support import Case, POLL_SECONDS, load_catalog, require_env, warm_case
from cleanup_live_db import age_test_records, broad_pairs, cleanup_metadata_expectations, head_object, identity_predicate, natural_keys, pair_predicate, pg_connect, repo_blobs_for_cases, selected_tags, snapshot


def seed_synthetic_shared(conn: psycopg.Connection[DictRow], spec: dict[str, str] | None, cases: list[Case], tags: list[DictRow]) -> str | None:
    if not spec:
        return None
    source_id = spec["source_id"]
    source_case = next((case for case in cases if case.id == source_id), None)
    if source_case is None:
        raise ValueError(f"synthetic shared source id not found: {source_id}")
    source = next(row for row in tags if (row["alias"], row["repository"], row["reference"]) == (source_case.alias, source_case.repository, source_case.reference))
    digest_value = source["digest"]
    alias, repo, reference, ecosystem = (spec[k] for k in ("alias", "repository", "reference", "ecosystem"))
    stamp = time.time_ns()
    with conn.cursor() as cur:
        cur.execute("SELECT * FROM meta_manifests WHERE alias=%s AND repository=%s AND digest=%s", (source_case.alias, source_case.repository, digest_value))
        manifest = cur.fetchone()
        if manifest is None:
            raise RuntimeError("source manifest missing for synthetic shared reference")
        cur.execute("INSERT INTO meta_manifests (id,key,alias,repository,reference,accept_key,digest,media_type,size,object_key,headers,expires_at,created_at,updated_at) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,0,%s,%s) ON CONFLICT(key) DO UPDATE SET reference=EXCLUDED.reference, accept_key=EXCLUDED.accept_key, created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at", (-910001, f"{alias}/{repo}@{digest_value}", alias, repo, reference, ecosystem, digest_value, manifest["media_type"], manifest["size"], manifest["object_key"], manifest["headers"], stamp, stamp))
        cur.execute("INSERT INTO meta_tags (id,key,alias,repository,reference,digest,expires_at,created_at,updated_at) VALUES (%s,%s,%s,%s,%s,%s,0,%s,%s) ON CONFLICT(key) DO UPDATE SET digest=EXCLUDED.digest, created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at", (-910002, f"{alias}/{repo}:{reference}", alias, repo, reference, digest_value, stamp, stamp))
        cur.execute("INSERT INTO meta_repo_blobs (id,key,alias,repository,digest,source_manifest,created_at,updated_at,last_access_at,last_verified_at) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s) ON CONFLICT(key) DO UPDATE SET digest=EXCLUDED.digest, source_manifest=EXCLUDED.source_manifest, created_at=EXCLUDED.created_at, updated_at=EXCLUDED.updated_at, last_access_at=EXCLUDED.last_access_at, last_verified_at=EXCLUDED.last_verified_at", (-910003, f"{alias}/{repo}@{digest_value}", alias, repo, digest_value, digest_value, stamp, stamp, stamp, stamp))
    conn.commit()
    print(f"SYNTHETIC shared CAS reference: {ecosystem}/{alias}/{repo}:{reference} -> {digest_value}")
    return digest_value


def touch_kept_cases(base: str, cases: list[Case], refresh: list[str]) -> None:
    by_id = {case.id: case for case in cases}
    for case_id in refresh:
        warm_case(base, by_id[case_id])


def post_cleanup(base: str) -> int:
    req = urllib.request.Request(urllib.parse.urljoin(base.rstrip("/") + "/", "admin/scheduler/cleanup"), data=b"", method="POST", headers={"Accept": "application/json"})
    user, password = os.getenv("REGIMUX_ADMIN_USERNAME"), os.getenv("REGIMUX_ADMIN_PASSWORD")
    if user and password:
        import base64
        req.add_header("Authorization", "Basic " + base64.b64encode(f"{user}:{password}".encode()).decode())
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            payload = json.loads(response.read())
            if response.status != 202 or payload != {"status": "accepted", "scope": "all"}:
                raise RuntimeError(f"cleanup response was {response.status}: {payload}")
            return response.status
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"cleanup trigger {base} returned {exc.code}: {exc.read().decode(errors='replace')}") from exc


def require_disposable_targets() -> None:
    dsn = urllib.parse.urlsplit(require_env("PG_DSN"))
    if (dsn.hostname, dsn.port, dsn.username, dsn.password, dsn.path) != ("postgres", 5432, "regimux", "acceptance-only", "/regimux"):
        raise RuntimeError("refusing mutation: PG_DSN is not the disposable acceptance database")
    if require_env("S3_BUCKET") != "regimux-acceptance":
        raise RuntimeError("refusing mutation: S3_BUCKET is not the disposable acceptance bucket")
    s3 = urllib.parse.urlsplit(require_env("S3_ENDPOINT"))
    if s3.scheme != "http" or s3.hostname not in {"s3", "minio", "localstack", "localstack-s3"} or s3.port not in {4566, 9000}:
        raise RuntimeError("refusing mutation: S3_ENDPOINT is not the local acceptance object store")
    expected = {"REGIMUX_BASE_URL": "regimux", "REGIMUX_PEER_URL": "regimux-peer"}
    for variable, hostname in expected.items():
        endpoint = urllib.parse.urlsplit(require_env(variable))
        if endpoint.scheme != "http" or endpoint.hostname != hostname or endpoint.port != 8080:
            raise RuntimeError(f"refusing mutation: {variable} is not an acceptance service endpoint")


def current_state(conn: psycopg.Connection[DictRow], cases: list[Case]) -> tuple[set[str], set[str]]:
    where, args = identity_predicate(cases)
    broad_where, broad_args = pair_predicate(broad_pairs(cases))
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SELECT alias,repository,reference FROM meta_tags WHERE {} OR {}").format(where, broad_where), [*args, *broad_args])
        existing_tags = {f"{r['alias']}|{r['repository']}|{r['reference']}" for r in cur.fetchall()}
        cur.execute("SELECT digest FROM meta_blobs")
        existing_digests = {row["digest"] for row in cur.fetchall()}
    return existing_tags, existing_digests


def expected_deleted_digests(before: dict[str, Any], cases: list[Case], shared_digest: str | None) -> set[str]:
    by_identity = {(c.alias, c.repository, c.reference): c.retention for c in cases}
    expired = expired_identities(before, cases)
    container_expired_pairs = {(c.alias, c.repository) for c in cases if c.ecosystem == "container" and c.retention == "expire"}
    expiring_pairs = {(c.alias, c.repository) for c in cases if c.retention == "expire"}
    kept_digests = {row["digest"] for row in before["tags"] if by_identity.get((row["alias"], row["repository"], row["reference"])) == "keep"}
    owned = {row["digest"] for row in before["tags"] if (row["alias"], row["repository"], row["reference"]) in expired}
    expired_sources = set(owned)
    owned.update(row["digest"] for row in before["repo_blobs"] if row["source_manifest"] in expired_sources or (row["alias"], row["repository"]) in container_expired_pairs)
    owned.update(row["digest"] for row in before["manifests"] if (row["alias"], row["repository"]) in container_expired_pairs)
    protected: set[str] = set()
    if shared_digest is not None:
        protected.add(shared_digest)
    for row in before["related_tags"]:
        identity = (row["alias"], row["repository"], row["reference"])
        if identity not in expired and (row["alias"], row["repository"]) not in container_expired_pairs:
            protected.add(row["digest"])
    for row in before["manifests"]:
        identity = (row["alias"], row["repository"], row["reference"])
        if identity not in expired and (row["alias"], row["repository"]) not in container_expired_pairs:
            protected.add(row["digest"])
    for row in before["repo_blobs"]:
        if row["source_manifest"] in kept_digests:
            protected.add(row["digest"])
        if row["source_manifest"] in expired_sources and (row["alias"], row["repository"]) not in expiring_pairs:
            protected.add(row["digest"])
    return owned - protected


def expired_identities(before: dict[str, Any], cases: list[Case]) -> set[tuple[str, str, str]]:
    by_identity = {(c.alias, c.repository, c.reference): c.retention for c in cases}
    expired = {identity for identity, retention in by_identity.items() if retention == "expire"}
    for row in before["tags"]:
        identity = (row["alias"], row["repository"], row["reference"])
        if (row["alias"], row["repository"]) in broad_pairs(cases) and by_identity.get(identity) != "keep":
            expired.add(identity)
    return expired


def wait_for_cleanup(
    conn: psycopg.Connection[DictRow], cases: list[Case], s3: S3Client,
    before: dict[str, Any], shared_digest: str | None, synthetic: dict[str, str] | None,
) -> None:
    expires = [c for c in cases if c.retention == "expire"]
    keeps = [c for c in cases if c.retention == "keep"]
    keep_digests = {row["digest"] for row in before["tags"] if any((row["alias"], row["repository"], row["reference"]) == (c.alias, c.repository, c.reference) for c in keeps)}
    delete_digests = expected_deleted_digests(before, cases, shared_digest)
    expired = expired_identities(before, cases)
    retired_manifests, retired_links, kept_manifests, kept_links, kept_pulls = cleanup_metadata_expectations(before, cases, expired)
    object_keys = {digest: info["key"] for digest, info in before["objects"].items() if digest in delete_digests}
    deadline = time.monotonic() + POLL_SECONDS
    removed = retained = metadata_removed = objects_removed = shared_ok = refs_removed = refs_kept = pulls_kept = False
    while time.monotonic() < deadline:
        tags, digests = current_state(conn, cases)
        expired_keys = {f"{alias}|{repo}|{reference}" for alias, repo, reference in expired_identities(before, cases)}
        removed = all(tag_key(c) not in tags for c in expires) and all(key not in tags for key in expired_keys)
        retained = all(tag_key(c) in tags for c in keeps)
        gone = {d for d in delete_digests if d not in digests}
        objects_removed = all(head_object(s3, object_keys[d]) is None for d in delete_digests if d in object_keys and d not in digests)
        shared_ok = shared_digest is None or shared_digest in digests and head_by_digest(s3, conn, shared_digest) is not None
        kept_ok = all(bool(tags_for_case(conn, c)) for c in keeps) and all(head_by_digest(s3, conn, digest) is not None for digest in keep_digests)
        metadata_removed = delete_digests <= gone
        current_manifests_removed = natural_keys(conn, "meta_manifests", ("alias", "repository", "digest"), retired_manifests)
        current_links_removed = natural_keys(conn, "meta_repo_blobs", ("alias", "repository", "digest"), retired_links)
        current_manifests_kept = natural_keys(conn, "meta_manifests", ("alias", "repository", "digest"), kept_manifests)
        current_links_kept = natural_keys(conn, "meta_repo_blobs", ("alias", "repository", "digest"), kept_links)
        current_pulls_kept = natural_keys(conn, "meta_pulls", ("alias", "repository", "reference"), kept_pulls)
        refs_removed = not current_manifests_removed and not current_links_removed
        refs_kept = kept_manifests <= current_manifests_kept and kept_links <= current_links_kept
        pulls_kept = kept_pulls <= current_pulls_kept
        synthetic_ok = synthetic_metadata_present(conn, synthetic, shared_digest)
        if removed and retained and objects_removed and shared_ok and kept_ok and metadata_removed and refs_removed and refs_kept and pulls_kept and synthetic_ok:
            return
        time.sleep(2)
    raise TimeoutError(f"cleanup did not converge: removed={removed}, retained={retained}, metadata={metadata_removed}, objects={objects_removed}, references_removed={refs_removed}, references_kept={refs_kept}, pull_history_kept={pulls_kept}, shared={shared_ok}")


def synthetic_metadata_present(
    conn: psycopg.Connection[DictRow], synthetic: dict[str, str] | None, digest: str | None,
) -> bool:
    if synthetic is None or digest is None:
        return True
    alias, repository, reference = (synthetic[key] for key in ("alias", "repository", "reference"))
    manifest_key = (alias, repository, digest)
    tag_key_value = (alias, repository, reference)
    return (
        manifest_key in natural_keys(conn, "meta_manifests", ("alias", "repository", "digest"), {manifest_key})
        and manifest_key in natural_keys(conn, "meta_repo_blobs", ("alias", "repository", "digest"), {manifest_key})
        and tag_key_value in natural_keys(conn, "meta_tags", ("alias", "repository", "reference"), {tag_key_value})
    )


def tag_key(case: Case) -> str:
    return f"{case.alias}|{case.repository}|{case.reference}"


def tags_for(conn: psycopg.Connection[DictRow], cases: list[Case]) -> list[DictRow]:
    where, args = identity_predicate(cases)
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SELECT * FROM meta_tags WHERE {}").format(where), args)
        return list(cur.fetchall())


def tags_for_case(conn: psycopg.Connection[DictRow], case: Case) -> list[DictRow]:
    return tags_for(conn, [case])


def blobs_for(conn: psycopg.Connection[DictRow], digests: set[str]) -> list[DictRow]:
    if not digests:
        return []
    with conn.cursor() as cur:
        cur.execute("SELECT * FROM meta_blobs WHERE digest = ANY(%s)", (list(digests),))
        return list(cur.fetchall())


def head_by_digest(s3: S3Client, conn: psycopg.Connection[DictRow], digest_value: str) -> dict[str, Any] | None:
    rows = blobs_for(conn, {digest_value})
    return head_object(s3, rows[0]["object_key"]) if rows else None


def main() -> None:
    cases, refresh, synthetic = load_catalog()
    require_disposable_targets()
    base = require_env("REGIMUX_BASE_URL")
    peer = require_env("REGIMUX_PEER_URL")
    conn = pg_connect()
    s3 = cast(S3Client, get_session().create_client(
        "s3", endpoint_url=require_env("S3_ENDPOINT"), region_name=os.getenv("AWS_REGION", "us-east-1"),
        config=Config(s3={"addressing_style": "path"}),
    ))
    for case in cases:
        warm_case(base, case)
    rows = selected_tags(conn, cases)
    before = snapshot(conn, cases, rows, s3)
    snapshot_path = Path(os.getenv("REGIMUX_ACCEPTANCE_CLEANUP_SNAPSHOT", "/tmp/regimux-cleanup-acceptance.json"))
    snapshot_data: dict[str, Any] = {}
    for name, value in before.items():
        if isinstance(value, list):
            rows_for_file = cast(list[DictRow], value)
            snapshot_data[name] = [{key: item for key, item in row.items() if key != "headers"} for row in rows_for_file]
        else:
            snapshot_data[name] = value
    snapshot_path.write_text(json.dumps(snapshot_data, default=str, indent=2), encoding="utf-8")
    repos = repo_blobs_for_cases(conn, cases)
    age_test_records(conn, cases, rows, repos)
    shared_digest = seed_synthetic_shared(conn, synthetic, cases, rows)
    touch_kept_cases(base, cases, refresh)
    trigger = os.getenv("REGIMUX_ACCEPTANCE_CLEANUP_TRIGGER", "manual").strip().lower()
    if trigger == "scheduled":
        print(f"PREPARED (await scheduler); snapshot={snapshot_path}", flush=True)
    elif trigger == "manual":
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            statuses = list(pool.map(post_cleanup, (base, peer)))
        print(f"MANUAL cleanup submissions: {statuses}")
    else:
        raise ValueError("REGIMUX_ACCEPTANCE_CLEANUP_TRIGGER must be 'manual' or 'scheduled'")
    wait_for_cleanup(conn, cases, s3, before, shared_digest, synthetic)
    if trigger == "manual":
        post_cleanup(peer)
        wait_for_cleanup(conn, cases, s3, before, shared_digest, synthetic)
        print(f"PASS cleanup accepted, converged, and repeated idempotently; snapshot={snapshot_path}")
    else:
        print(f"PASS scheduled cleanup converged; snapshot={snapshot_path}")
    conn.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"FAIL cleanup acceptance: {exc}", file=sys.stderr)
        raise

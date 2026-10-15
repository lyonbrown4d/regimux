"""Initialize the disposable S3 bucket and verify the real backing services."""
from __future__ import annotations

import os
import time
from typing import cast

import psycopg
from botocore.session import get_session
from botocore.config import Config
from botocore.exceptions import BotoCoreError, ClientError
from mypy_boto3_s3 import S3Client


def main() -> None:
    bucket = os.environ["S3_BUCKET"]
    if bucket != "regimux-acceptance":
        raise ValueError("bootstrap is restricted to the disposable acceptance bucket")
    client = cast(S3Client, get_session().create_client(
        "s3",
        endpoint_url=os.environ["S3_ENDPOINT"],
        config=Config(signature_version="s3v4", s3={"addressing_style": "path"}),
    ))
    deadline = time.monotonic() + 120
    while True:
        try:
            buckets = client.list_buckets()["Buckets"]
            break
        except (BotoCoreError, ClientError):
            if time.monotonic() >= deadline:
                raise
            time.sleep(2)
    if not any(item.get("Name") == bucket for item in buckets):
        client.create_bucket(Bucket=bucket)
    client.head_bucket(Bucket=bucket)
    with psycopg.connect(os.environ["PG_DSN"]) as conn:
        with conn.cursor() as cursor:
            cursor.execute("SELECT 1")
            assert cursor.fetchone() == (1,)
    print("PASS: real S3 bucket and PostgreSQL are ready", flush=True)


if __name__ == "__main__":
    main()

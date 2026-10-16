"""Create the private fixture bucket using the existing acceptance boto3 client."""
from __future__ import annotations

import time

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError, EndpointConnectionError


def main() -> None:
    client = boto3.client(
        "s3", endpoint_url="http://s3:9000", region_name="us-east-1",
        config=Config(
            s3={"addressing_style": "path"}, retries={"max_attempts": 0},
            connect_timeout=2, read_timeout=5,
        ),
    )
    bucket = "regimux-concurrency"
    for attempt in range(60):
        try:
            client.head_bucket(Bucket=bucket)
            return
        except ClientError as error:
            if error.response["Error"]["Code"] not in ("404", "NoSuchBucket"):
                raise
        except EndpointConnectionError:
            time.sleep(2)
            continue
        try:
            client.create_bucket(Bucket=bucket)
            return
        except (ClientError, EndpointConnectionError):
            if attempt == 59:
                raise
            time.sleep(2)
    raise TimeoutError("private S3 fixture did not become ready")


if __name__ == "__main__":
    main()

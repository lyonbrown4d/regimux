# RegiMux k6 stress tests

The stress suite runs against the Docker Compose integration environment and
uses the same real artifacts as the integration tests:

- npm: `lodash@4.17.21`
- PyPI: `six==1.17.0`
- Maven: `commons-io:commons-io:2.16.1`
- Container: `busybox:1.36.1`

## Commands

- `task stress:smoke`: short sqlite-backed script validation.
- `task stress`: default sqlite-backed load profile.
- `task stress:mysql`: load profile with MySQL metadata storage.
- `task stress:postgres`: load profile with Postgres metadata storage.
- `task stress:databases`: load profile across sqlite, MySQL, and Postgres.

Reports are written to `tests/stress/reports` as Markdown and JSON. The
generated report files are ignored by git because they are local environment
measurements.

## Profiles

- `smoke`: quick validation, low concurrency.
- `load`: default report profile.
- `stress`: higher concurrency profile for manual runs.

To run a custom profile against an already-started integration environment:

```sh
REGIMUX_STRESS_PROFILE=stress task stress:run
```

The report includes isolated hot-cache scenarios and one mixed scenario where
npm, PyPI, Maven, and container requests share the same RegiMux instance.

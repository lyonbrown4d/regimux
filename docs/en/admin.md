# Admin UI

The Admin UI is embedded into the RegiMux binary. It uses Fiber template rendering, embedded templates, embedded i18n resources, Tailwind CSS from CDN, and htmx from CDN.

Open:

```text
http://localhost:5000/admin
```

## Security

When `auth.enabled = true`, Admin UI is protected with HTTP Basic using the same configured users as Registry auth.

## Language and Theme

Admin UI supports English and Chinese. Locale resources are embedded in the binary.

The UI follows the browser or operating system light/dark preference automatically.

## Views

Current views:

- dashboard
- upstream health
- pull and activity history
- cache status
- storage and large blobs
- scheduler jobs, prefetch runs, and prefetch outcomes
- manual sync
- auth audit
- effective configuration

## Manual Sync

Manual sync warms an image through the configured cache path:

```text
{containerAlias}/library/node:20
{containerAlias}/gitlab/gitlab-ce:latest
```

It fetches the manifest and referenced blobs, then records outcomes in metadata.

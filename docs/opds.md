# OPDS

Voltis serves OPDS catalogs so reading apps can browse your libraries, lists
and search results, download comics and books, and stream comic pages.

## Keys and feed URLs

OPDS apps sign in with a per-user key instead of a session. Create one under
**Settings → OPDS**, one per app or device. The key's link button shows its two
feed URLs:

| Feed     | URL                                                  | Use with                                                      |
| -------- | ---------------------------------------------------- | ------------------------------------------------------------- |
| OPDS 1.2 | `https://voltis.example.com/opds/<key>/v1.2/catalog` | KOReader, Panels, Chunky and other apps that support OPDS-PSE |
| OPDS 2.0 | `https://voltis.example.com/opds/<key>/v2/catalog`   | Readium-based apps, such as Thorium                           |

Pick 1.2 unless your app only speaks 2.0: page streaming and progress sync
exist only in the 1.2 feed.

::: warning The key is a password
The key is part of the URL, so anyone who sees the URL can read your libraries
and change your progress. Voltis keeps keys out of its own logs, but a reverse
proxy's access log records the full path.

Keys are independent of your session: signing out, changing your password or
changing your linked identity does not revoke them. Revoke a key on the OPDS
settings page when a device is lost or retired. Deleting the user removes all
of their keys.
:::

## Reading progress

Apps that stream comics page by page through OPDS-PSE update your progress as
they fetch pages:

- Progress only moves forward. Going back to an earlier page leaves it where it
  was.
- Page 1 records nothing unless it is the only page, since apps fetch it as a
  preview.
- Fetching the last page marks the comic _completed_, unless its status is
  already something other than _reading_.

Some apps fetch every page of a comic up front to read it offline. Voltis cannot
tell that from reading, so the comic is marked completed as soon as the
download finishes. Downloads and the 2.0 feed never change progress.

Pages are sent as JPEG unless every page of the comic is PNG or every page is
GIF. Other pages are converted.

## Reverse proxies

Feed links are absolute. Behind a proxy that rewrites the `Host` header, set
`app.public_url` so the links point at the address apps connect to.

With [forwarded authentication](/authentication#forwarded-authentication), apps
cannot get past your proxy's login page. Let `/opds/` through without
authentication: the key in the path authenticates those requests, and Voltis
ignores identity headers there. In Caddy, handle that path before the
`forward_auth` route:

```caddyfile
handle /opds/* {
	reverse_proxy voltis:8080
}
```

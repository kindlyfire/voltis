# Command-line reference

The Voltis binary is available inside the Docker container. You can run commands
directly:

```bash
docker compose exec app ./voltis <command>
```

Or open a shell first:

```bash
docker compose exec app sh
./voltis <command>
```

## Environment variables

| Variable                   | Default             | Description                                                                               |
| -------------------------- | ------------------- | ----------------------------------------------------------------------------------------- |
| `APP_DATABASE_URL`         | _(required)_        | PostgreSQL connection URL                                                                 |
| `APP_HOST`                 | _(empty)_           | HTTP server listen address; empty listens on all interfaces                               |
| `APP_PORT`                 | `8080`              | HTTP server port                                                                          |
| `APP_CACHE_DIR`            | `/tmp/voltis_cache` | Directory for cached cover images. Mount it to keep covers when the container is recreated |
| `APP_REGISTRATION_ENABLED` | `false`             | Allow open user registration. If no accounts exist, one user will be allowed to register¹ |
| `APP_STATIC_DIR`           | _(empty)_           | Path to frontend static files (set automatically in the Docker image)                     |

Forwarded authentication is configured here. See
[Authentication](/authentication#forwarded-authentication) for the setup details.

| Variable                       | Default   | Description                                                             |
| ------------------------------ | --------- | ----------------------------------------------------------------------- |
| `APP_AUTH_PROXY_TRUSTED_CIDRS` | _(empty)_ | CIDRs allowed to assert identity headers. Empty disables forwarded auth |
| `APP_AUTH_PROXY_USER_HEADER`   | _(empty)_ | Header carrying the username, for example `Remote-User`                 |
| `APP_AUTH_PROXY_EMAIL_HEADER`  | _(empty)_ | Header carrying the email address                                       |
| `APP_AUTH_PROXY_GROUPS_HEADER` | _(empty)_ | Header carrying the group list                                          |

¹ `APP_REGISTRATION_ENABLED` only seeds `auth.registration_enabled` the first time
Voltis starts against a database. After that the stored setting wins.

## Commands

### `server`

Starts the HTTP server. This is what the container runs by default.

```bash
./voltis server
```

### `metadata match`

Matches every pending series of a library with the metadata providers, even when
[automatic matching](/metadata#automatic-matching) is off for it. Copy the
library ID from its row under Settings → Libraries.

```bash
./voltis metadata match --library <id> [--dry-run]
```

- `--dry-run` prints each decision without saving it: `linked`, `review`,
  `unmatched` or `failed`
- Libraries being scanned are skipped

### `users create`

Creates a new user.

```bash
./voltis users create <username> (--password <password> | --no-password) [--admin]
```

- `--password` sets the password. Use `-` to read from stdin
- `--no-password` creates an account without one. The first OIDC or proxy login
  matched to it [claims it](/authentication#linking-external-logins-to-existing-accounts)
- `--admin` grants admin permissions
- Passwords must be at least 8 characters

### `users link`

Links an external identity to an existing user, so that login signs in as that
user.

```bash
./voltis users link <username> --provider proxy --subject <proxy-username>
./voltis users link <username> --provider oidc --issuer <issuer-url> --subject <sub>
```

- For `proxy`, the subject is the username the proxy sends
- For `oidc`, the issuer must equal `auth.oidc.issuer` and the subject is the
  ID token's `sub` claim
- Fails if the identity is already linked to another user

### `settings list`

Lists every setting with its current value. Secrets are shown as `<set>` or
`<not set>`, never their value.

```bash
./voltis settings list
```

### `settings get`

```bash
./voltis settings get <key>
```

### `settings set`

Changes a setting. A running server picks the change up immediately.

```bash
./voltis settings set auth.password_login_enabled true
./voltis settings set auth.oidc.scopes "openid profile email groups"
```

See [Authentication](/authentication#settings) for the full options list.

### `users update`

Updates an existing user.

```bash
./voltis users update <username> [--username <new>] [--password <new>] [--admin | --no-admin]
```

All flags are optional.

### `identities set-issuer`

Moves every OIDC identity from one issuer URL to another, for a provider whose
URL changed but whose subjects did not.

```bash
./voltis identities set-issuer <old-issuer> <new-issuer>
```

If any subject already exists under the new issuer, it changes nothing and lists
the affected users. See
[Changing the provider](/authentication#changing-the-provider).

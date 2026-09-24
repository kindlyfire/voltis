# Local auth test stacks

Two optional compose profiles for exercising the sign-in methods against real
providers. Neither is started by a plain `docker compose up`.

Both assume Voltis itself runs on the host with `just app server`, reachable on
`http://localhost:8080`.

| Service  | Port  | Profile      |
| -------- | ----- | ------------ |
| Dex      | 5556  | `oidc`       |
| Caddy    | 9443  | `proxy-auth` |
| Authelia | 9091  | `proxy-auth` |

All three bind to `127.0.0.1` only. Postgres keeps whatever port your compose
invocation gives it; in this worktree that is 5442.

> Add `--profile` to the same `docker compose` invocation you already use, so
> the services land in the same project as your database. Use `stop`, not
> `down`: `down` removes every container in the project, the database included.

---

## OIDC, with Dex

### Bring it up

```bash
docker compose --profile oidc up -d dex
curl -s http://localhost:5556/dex/.well-known/openid-configuration | head -3
```

### Configure Voltis

```bash
cd backend
go run . settings set app.public_url http://localhost:8080
go run . settings set auth.oidc.issuer http://localhost:5556/dex
go run . settings set auth.oidc.client_id voltis
go run . settings set auth.oidc.client_secret voltis-secret
go run . settings set auth.oidc.username_claim name
go run . settings set auth.oidc.scopes "openid,profile,email,groups"
go run . settings set auth.oidc.enabled true

# For the linking and admin-group runs
go run . settings set auth.link.match_username true
go run . settings set auth.link.match_email true
go run . settings set auth.admin_group authors
```

Two of those are not optional:

- `auth.oidc.username_claim` must be `name`. Dex does not emit
  `preferred_username`, so the default leaves every login on the "pick a
  username" page — a valid run of its own, but not the one you want first.
- `groups` must be in the scopes. Dex only puts the `groups` claim in the token
  when the client asks for that scope, and without it `auth.admin_group` never
  matches. The CLI takes the list comma or space separated and stores a JSON
  array.

### Users

Dex asks for the **email address**, not the username.

| Login               | Password   | `name` claim    | Groups      |
| ------------------- | ---------- | --------------- | ----------- |
| `alice@example.com` | `password` | `alice`         | _(none)_    |
| `bob@example.com`   | `password` | `bob`           | _(none)_    |
| _Mock user_ button  | _(none)_   | `Kilgore Trout` | `authors`   |

Dex's local users cannot carry groups, so the mock connector — one click, no
password — provides the identity that has one. That is the account to use for
`auth.admin_group`.

### Walk through

Visit <http://localhost:8080/auth/login> and press the SSO button.

- **Auto-create:** sign in as `bob@example.com` on a clean database. A `bob`
  account appears with his email.
- **Username matching:** create a local `alice` with a password first, then sign
  in as `alice@example.com`. Voltis asks for that account's password to link,
  and offers "not my account".
- **Pre-created account:** create `alice` from Settings → Users without a
  password, then sign in as her. The link happens with no prompt.
- **Email matching:** set a local account's email to `bob@example.com` and sign
  in as Bob. The accounts link without a prompt.
- **Admin group:** with `auth.admin_group=authors`, use the mock user. The
  account uses the name `Kilgore Trout` and is granted ADMIN.
- **Self-link:** while signed in, Settings → Account → Connect SSO.

### Tear down

```bash
docker compose stop dex && docker compose rm -f dex
```

---

## Forwarded auth, with Authelia and Caddy

Caddy sits in front of Voltis, Authelia authenticates, and Caddy passes
`Remote-User`, `Remote-Email` and `Remote-Groups` to the app — after stripping
whatever the client sent under those names.

Authelia refuses to protect a plain-http site, so Caddy serves https with its
own CA. **The browser warns on the first visit; accept and proceed.** Both the
app and the Authelia portal live on that one origin, which is also what keeps
the session cookie working.

### Bring it up

```bash
docker compose --profile proxy-auth up -d authelia caddy
```

Restart Voltis on the host with the trust settings:

```bash
APP_AUTH_PROXY_TRUSTED_CIDRS=172.16.0.0/12 \
APP_AUTH_PROXY_USER_HEADER=Remote-User \
APP_AUTH_PROXY_EMAIL_HEADER=Remote-Email \
APP_AUTH_PROXY_GROUPS_HEADER=Remote-Groups \
just app server
```

`172.16.0.0/12` covers Docker's default bridge ranges, which is where Caddy
connects from. Do not widen it.

### Configure Voltis

```bash
cd backend
go run . settings set app.public_url https://localhost:9443
go run . settings set auth.admin_group admins
go run . settings set auth.proxy.logout_url https://localhost:9443/authelia/logout
```

### Users

| Username | Password   | Email               | Groups          |
| -------- | ---------- | ------------------- | --------------- |
| `alice`  | `password` | `alice@example.com` | `admins`, `users` |
| `bob`    | `password` | `bob@example.com`   | `users`         |

### Walk through

Visit <https://localhost:9443> and accept the certificate warning.

- **Header login:** sign in as `alice`. Authelia redirects back and Voltis has
  provisioned `alice` with her email, no password, and ADMIN from the `admins`
  group.
- **Not an admin:** sign out at
  <https://localhost:9443/authelia/logout>, sign in as `bob`. No ADMIN, and
  Settings shows no admin sections.
- **User switch:** signing in as the other user in the same browser replaces the
  Voltis session and drops the previous user's live connections.
- **Header stripping:** the point of the whole setup. With a signed-in browser,
  send a forged header and confirm the app ignores it:

  ```bash
  curl -k -b <cookies> -H 'Remote-User: eve' https://localhost:9443/api/users/me
  ```

  It answers as the signed-in user, and no `eve` account is created.
- **No bypass:** stop Caddy and hit `http://localhost:8080` directly. The
  proxy-issued cookie is refused, because a proxy session without its header is
  not accepted.
- **Logout:** the logout button appears only because
  `auth.proxy.logout_url` is set, and it leaves for Authelia.
- **WebSocket:** the connection authenticates through the same headers; watch
  Settings → Tasks update live while a scan runs.

### Tear down

```bash
docker compose stop caddy authelia && docker compose rm -f caddy authelia
```

---

## Files

- `dex/config.yaml` — issuer, static client, static users, mock connector
- `authelia/configuration.yml` — pinned to 4.37, which still accepts a
  `localhost` cookie domain
- `authelia/users.yml` — the two users and their groups
- `caddy/Caddyfile` — header stripping, `forward_auth`, and the https listener

None of these carry real secrets; they exist to be committed.

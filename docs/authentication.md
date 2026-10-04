# Authentication

Voltis supports three ways of signing in, and they can be used together:

- **Username and password**, stored by Voltis
- **Single sign-on (OIDC)** against a provider such as Authentik, Keycloak,
  Dex or Auth0
- **Forwarded authentication**, where a reverse proxy such as Authelia,
  Authentik or oauth2-proxy authenticates the user and passes their identity to
  Voltis in HTTP headers

Everything except the forwarded-auth header names is configured under **Settings
→ General** in the web interface, or with [`voltis settings`](/cli#settings).

[OPDS apps](/opds) use per-user keys instead of these methods. Other apps sign
in with them and then keep their own session; see [Apps](#apps).

## Settings

| Key                              | Type   | Default                | Meaning                                                            |
| -------------------------------- | ------ | ---------------------- | ------------------------------------------------------------------ |
| `app.public_url`                 | string | _(empty)_              | Externally reachable base URL. Required for OIDC                   |
| `auth.registration_enabled`      | bool   | `false`                | Allow anyone to create an account                                  |
| `auth.password_login_enabled`    | bool   | `true`                 | Allow username and password login                                  |
| `auth.external_auto_create`      | bool   | `true`                 | Create an account on first OIDC or proxy login                     |
| `auth.external_session_max_days` | int    | `30`                   | Hard lifetime of a browser session created by OIDC or a proxy      |
| `auth.oidc.enabled`              | bool   | `false`                | Enable single sign-on                                              |
| `auth.oidc.issuer`               | string | _(empty)_              | Issuer URL, used for discovery                                     |
| `auth.oidc.client_id`            | string | _(empty)_              | Client ID                                                          |
| `auth.oidc.client_secret`        | secret | _(empty)_              | Client secret. Write-only: never returned by the API or the CLI    |
| `auth.oidc.scopes`               | list   | `openid profile email` | Scopes requested from the provider                                 |
| `auth.oidc.button_label`         | string | `Sign in with SSO`     | Label of the login button                                          |
| `auth.oidc.auto_redirect`        | bool   | `false`                | Send users straight to the provider from the login page            |
| `auth.oidc.username_claim`       | string | `preferred_username`   | Claim holding the username                                         |
| `auth.oidc.groups_claim`         | string | `groups`               | Claim holding the group list                                       |
| `auth.admin_group`               | string | _(empty)_              | Group granting admin. Empty disables the mapping                   |
| `auth.link.match_username`       | bool   | `false`                | Link an external login to the local account with the same username |
| `auth.link.match_email`          | bool   | `false`                | Link an external login to the local account with the same email    |
| `auth.proxy.logout_url`          | string | _(empty)_              | Where proxy users go on logout. Empty hides the logout button      |

Settings are stored in the database, so a change made from the CLI is picked up
by a running server immediately.

## Single sign-on (OIDC)

1. Set `app.public_url` to the URL users reach Voltis on. The redirect URI is
   that URL plus `/api/auth/oidc/callback`, and Settings → General shows the
   exact value to paste into your provider.
2. Register a confidential client there with that redirect URI.
3. Fill in the issuer, client ID and client secret, and enable single sign-on.

::: tip Group mapping depends on the provider
If `auth.admin_group` never matches, check how your provider exposes groups and
that `auth.oidc.groups_claim` names the emitted claim, then sign in again:

- [Dex](https://dexidp.io/docs/configuration/custom-scopes-claims-clients/)
  requires the `groups` scope; set `auth.oidc.scopes` to
  `openid profile email groups`.
- [Authentik](https://docs.goauthentik.io/add-secure-apps/providers/oauth2/)
  includes group membership in its default `profile` scope mapping.
- [Keycloak](https://www.keycloak.org/docs/latest/server_admin/#_client_scopes)
  needs a group membership mapper. Mappers in a default client scope apply
  without explicitly requesting that scope; optional client scopes must be
  requested in `auth.oidc.scopes`.
  :::

::: warning Missing groups demote
When `auth.admin_group` is set, groups missing from both the ID token and
`userinfo` count as no groups, and the sign-in demotes the user, even the last
admin. Only a failed `userinfo` request refuses the sign-in instead. Check the
claim before setting the group, and see [Losing access](#losing-access).
:::

### Changing the provider

Disabling single sign-on, or changing the issuer or client ID, signs out every
OIDC session and cancels sign-ins in progress.

Identities are keyed by issuer and subject. To move to a new issuer URL for the
same provider, where subjects stay the same, set `auth.oidc.issuer` and then
run [`voltis identities set-issuer`](/cli#identities-set-issuer) before users
sign in again:

```bash
./voltis settings set auth.oidc.issuer https://new.example.com
./voltis identities set-issuer https://old.example.com https://new.example.com
```

## Forwarded authentication

Forwarded auth is configured through environment variables.

| Variable                       | Default   | Description                                                                                      |
| ------------------------------ | --------- | ------------------------------------------------------------------------------------------------ |
| `APP_AUTH_PROXY_TRUSTED_CIDRS` | _(empty)_ | Comma or space separated CIDRs allowed to assert identity headers. Empty disables forwarded auth |
| `APP_AUTH_PROXY_USER_HEADER`   | _(empty)_ | Header carrying the username, for example `Remote-User`. Required when CIDRs are set             |
| `APP_AUTH_PROXY_EMAIL_HEADER`  | _(empty)_ | Header carrying the email address, for example `Remote-Email`                                    |
| `APP_AUTH_PROXY_GROUPS_HEADER` | _(empty)_ | Header carrying the group list, for example `Remote-Groups`                                      |

Voltis refuses to start if the CIDR list contains `0.0.0.0/0` or `::/0`.

The trust check uses the **TCP peer address** of the connection, never
`X-Forwarded-For`, which any client can set. List only the addresses your proxy
actually connects from.

::: tip IPv6
The peer address is whatever the proxy connects from, and a proxy reaching
Voltis over `localhost` can arrive as `::1` rather than `127.0.0.1`. If both
are possible, list both families:
`APP_AUTH_PROXY_TRUSTED_CIDRS=127.0.0.0/8,::1/128`.
:::

### Your proxy must strip the identity headers

::: danger
Your proxy must **strip or overwrite every configured identity header on every
request**, including the email and groups headers when it has no value for
them.
:::

The peer check proves that the _connection_ came from your proxy. It cannot
prove that the _headers_ on that connection were written by your proxy.

A Caddy example, with the strip before the authentication step:

```caddyfile
route {
	request_header -Remote-User
	request_header -Remote-Email
	request_header -Remote-Groups

	forward_auth authelia:9091 {
		uri /api/verify?rd=https://auth.example.com/
		copy_headers Remote-User Remote-Groups Remote-Email
	}

	reverse_proxy voltis:8080
}
```

## Linking external logins to existing accounts

When an external login arrives that Voltis has not seen before, it resolves in
this order:

1. An identity already linked to an account: sign in as that account.
2. `auth.link.match_email`, if one account has the address. OIDC addresses
   count only when the provider marks them `email_verified`; the proxy's email
   header is trusted as sent.
3. `auth.link.match_username`, if one account has that username.
4. `auth.external_auto_create`: create a new account. This is not governed by
   `auth.registration_enabled`. When the username is taken, OIDC users pick
   another one and proxy requests are refused.

What a match in steps 2 and 3 does depends on the account:

| Matched account                     | OIDC                         | Proxy       |
| ----------------------------------- | ---------------------------- | ----------- |
| No password and no linked identity  | Linked                       | Linked      |
| Has a password                      | Linked after password prompt | Not matched |
| No password, already has identities | Not matched                  | Not matched |

An account that is not matched falls through to the next step. The first kind
is one an admin pre-created for this user, with
[`users create --no-password`](/cli#users-create) or from the web interface.

Through a proxy, an existing account with a password is never linked
automatically. Before enabling forwarded auth on a server with password
accounts, link each one with [`voltis users link`](/cli#users-link):

```bash
./voltis users link alice --provider proxy --subject alice
```

The subject is the username the proxy sends. To prepare accounts for proxy
users instead, create them with `./voltis users create <name> --no-password`
and set `auth.link.match_username` to `true`, or link them as above.
An account missed here locks its user out; see [Losing access](#losing-access).

::: warning Email matching trusts the provider
`auth.link.match_email` is off by default, and enabling it means trusting the
identity provider.

Local addresses are never verified by Voltis. Anyone who can register a matching
verified address at the provider can claim a pre-created account that uses it,
or be asked for the password of an account that has one.
:::

## Apps

An app signs in one of two ways:

- **Password**, entered in the app. This needs `auth.password_login_enabled`.
- **Sign in with browser.** The app opens `/authorize-app` on the server in a
  browser. You sign in there with any method above, confirm, and the browser
  hands the app a one-time code.

Either way the app gets a **device session**. It is sent as
`Authorization: Bearer <token>` instead of a cookie, and it ends after 30 days
without use. `auth.external_session_max_days` does not apply to it, so an app
in regular use stays signed in.

**Settings → Account** lists your sessions and can sign out any but the current
one.

::: warning Device sessions outlive changes at the provider
A device session minted through an OIDC or proxy login is not checked against
the provider or the proxy again. A user who is disabled or removed there keeps
access from the app for as long as it is used at least every 30 days, and their
current admin status, which is no longer synced from the provider's groups.

To take admin rights away, edit the user's permissions under **Settings →
Users**. That applies immediately.

To end the access, an admin can:

- delete the account;
- with password login enabled, set a password for the user and then unlink
  their external identity, which ends all their sessions;
- disable single sign-on or change its issuer or client ID, which ends every
  OIDC session on the server. Proxy device sessions stop working while
  forwarded auth is disabled.

Users can sign out their own sessions, but an admin cannot list or end
another user's.
:::

### Behind a login proxy

Signing in with the browser goes through the proxy like any other page. The
app's later requests carry only its token, so the proxy has to let them through
without a login. Bypass the proxy's authentication for these requests only:

- any request whose `Authorization` header matches `^Bearer [0-9a-f]{64}$`.
  Don't match `Bearer *`: Voltis ignores any other value and uses the cookie,
  so those requests would skip the proxy;
- `GET /api/info`, which the app uses to check the server address;
- `POST /api/auth/token/exchange`, which completes a browser sign-in;
- `POST /api/auth/token`, only if users sign in with a password in the app.

The bypass must still [strip the identity
headers](#your-proxy-must-strip-the-identity-headers). In Caddy:

```caddyfile
@app header_regexp Authorization "^Bearer [0-9a-f]{64}$"
@app_info {
	method GET
	path /api/info
}
@app_signin {
	method POST
	path /api/auth/token /api/auth/token/exchange
}

route {
	request_header -Remote-User
	request_header -Remote-Email
	request_header -Remote-Groups

	reverse_proxy @app voltis:8080
	reverse_proxy @app_info voltis:8080
	reverse_proxy @app_signin voltis:8080

	forward_auth authelia:9091 {
		uri /api/verify?rd=https://auth.example.com/
		copy_headers Remote-User Remote-Groups Remote-Email
	}

	reverse_proxy voltis:8080
}
```

## Losing access

Everything needed to recover is available from the CLI:

```bash
# Password login was disabled and the provider is unreachable
./voltis settings set auth.password_login_enabled true

# The last admin was demoted by a group mapping, for example after
# auth.admin_group was set but the provider sends no groups. Fix the groups
# claim first, or clear the mapping, or the next login demotes again
./voltis settings set auth.admin_group ""
./voltis users update myuser --admin

# Forwarded auth: every request fails with "the username myuser is already
# taken", because a password account has that name
./voltis users link myuser --provider proxy --subject myuser

# Forwarded auth under a different username: the proxy user got a new account
# without admin. If the proxy sends groups, they must include auth.admin_group
./voltis users update proxy-name --admin
```

# Authentication

Voltis supports three ways of signing in, and they can be used together:

- **Username and password**, stored by Voltis
- **Single sign-on (OIDC)** against a provider such as Authentik, Keycloak,
  Dex or Auth0
- **Forwarded authentication**, where a reverse proxy such as Authelia,
  Authentik or oauth2-proxy authenticates the user and passes their identity to
  Voltis in HTTP headers

Everything except the forwarded-auth trust boundary is configured under
**Settings → General** in the web interface, or with
[`voltis settings`](/cli#settings).

## Settings

| Key                              | Type   | Default              | Meaning                                                           |
| -------------------------------- | ------ | -------------------- | ----------------------------------------------------------------- |
| `app.public_url`                  | string | _(empty)_            | Externally reachable base URL. Required for OIDC                  |
| `auth.registration_enabled`       | bool   | `false`              | Allow anyone to create an account                                 |
| `auth.password_login_enabled`     | bool   | `true`               | Allow username and password login                                 |
| `auth.external_auto_create`       | bool   | `true`               | Create an account on first OIDC or proxy login                    |
| `auth.external_session_max_days`  | int    | `30`                 | Hard lifetime of a session created by OIDC or a proxy             |
| `auth.oidc.enabled`               | bool   | `false`              | Enable single sign-on                                             |
| `auth.oidc.issuer`                | string | _(empty)_            | Issuer URL, used for discovery                                    |
| `auth.oidc.client_id`             | string | _(empty)_            | Client ID                                                         |
| `auth.oidc.client_secret`         | secret | _(empty)_            | Client secret. Write-only: never returned by the API or the CLI   |
| `auth.oidc.scopes`                | list   | `openid profile email` | Scopes requested from the provider                              |
| `auth.oidc.button_label`          | string | `Sign in with SSO`   | Label of the login button                                         |
| `auth.oidc.auto_redirect`         | bool   | `false`              | Send users straight to the provider from the login page           |
| `auth.oidc.username_claim`        | string | `preferred_username` | Claim holding the username                                        |
| `auth.oidc.groups_claim`          | string | `groups`             | Claim holding the group list                                      |
| `auth.admin_group`                | string | _(empty)_            | Group granting admin. Empty disables the mapping                  |
| `auth.link.match_username`        | bool   | `false`              | Link an external login to the local account with the same username |
| `auth.link.match_email`           | bool   | `false`              | Link an external login to the local account with the same email   |
| `auth.proxy.logout_url`           | string | _(empty)_            | Where proxy users go on logout. Empty hides the logout button     |

Settings are stored in the database and shared by every Voltis process, so a
change made from the CLI is picked up by a running server immediately.

## Single sign-on (OIDC)

1. Set `app.public_url` to the URL users reach Voltis on. The redirect URI is
   that URL plus `/api/auth/oidc/callback`, and Settings → General shows the
   exact value to paste into your provider.
2. Register a confidential client there with that redirect URI.
3. Fill in the issuer, client ID and client secret, and enable single sign-on.

Voltis validates the ID token's signature, issuer, audience, expiry and nonce,
and uses PKCE. The `userinfo` endpoint is only consulted when the username,
email or groups claim is missing from the ID token.

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

::: warning
If `auth.admin_group` is set and the ID token carries no groups claim, Voltis
must reach `userinfo` to know whether the user is still an admin. When that
request fails the sign-in is refused, because an outage and a removed group are
indistinguishable. Put the groups claim in the ID token to avoid depending on
it.
:::

## Forwarded authentication

Forwarded auth is configured **in the environment, not in the database**: a
wrong value here lets anyone take over any account, so it is deliberately out
of reach of the Settings page.

| Variable                        | Default   | Description                                                        |
| ------------------------------- | --------- | ------------------------------------------------------------------ |
| `APP_AUTH_PROXY_TRUSTED_CIDRS`  | _(empty)_ | Comma or space separated CIDRs allowed to assert identity headers. Empty disables forwarded auth |
| `APP_AUTH_PROXY_USER_HEADER`    | _(empty)_ | Header carrying the username, for example `Remote-User`. Required when CIDRs are set |
| `APP_AUTH_PROXY_EMAIL_HEADER`   | _(empty)_ | Header carrying the email address, for example `Remote-Email`      |
| `APP_AUTH_PROXY_GROUPS_HEADER`  | _(empty)_ | Header carrying the group list, for example `Remote-Groups`        |

Voltis refuses to start if the CIDR list contains `0.0.0.0/0` or `::/0`.

The trust check uses the **TCP peer address** of the connection, never
`X-Forwarded-For`, which any client can set. List only the addresses your proxy
actually connects from.

::: tip IPv6
The peer address is whatever the proxy connects from, and a proxy reaching
Voltis over `localhost` often arrives as `::1` rather than `127.0.0.1`. If both
are possible, list both families:
`APP_AUTH_PROXY_TRUSTED_CIDRS=127.0.0.0/8,::1/128`.
:::

### Your proxy must strip the identity headers

::: danger This is the requirement the whole model rests on
Your proxy must **strip or overwrite every configured identity header on every
request**, including the email and groups headers when it has no value for
them.
:::

The peer check proves that the *connection* came from your proxy. It cannot
prove that the *headers* on that connection were written by your proxy. If a
client sends `Remote-User: admin` and the proxy forwards the request without
touching that header, Voltis receives it from a trusted peer and has no way to
tell it apart from an identity the proxy asserted itself. Anyone who can reach
the proxy could then sign in as anyone.

Clearing only the username header is not enough. If the proxy sets
`Remote-User` but passes a client-supplied `Remote-Groups` through, the client
chooses their own group membership, and with `auth.admin_group` configured that
means choosing to be an admin.

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

### How a proxy login behaves

- The header wins. If the session cookie belongs to a different user, or was
  not issued to a proxy login, it is replaced and the previous user's live
  connections are dropped.
- A session issued to a proxy login is rejected when the header is absent, so
  a cookie obtained through the proxy cannot be replayed against Voltis
  directly.
- Email and group membership are synced on every request, and written only when
  they change.
- A username header that is empty or sent more than once is rejected.
- Logout sends the user to `auth.proxy.logout_url`. Without it there is nothing
  a logout button could do, so it is hidden.

## Linking external logins to existing accounts

When an external login arrives that Voltis has not seen before, it resolves in
this order:

1. An identity already linked to an account: sign in as that account.
2. `auth.link.match_email`, if the address is verified and one account has it.
3. `auth.link.match_username`, if one account has that username. Through OIDC
   the user must confirm with that account's password, unless the account has
   no password and no identities yet — an account an admin pre-created for
   them. Through a proxy the link is automatic.
4. `auth.external_auto_create`: create a new account. This is not governed by
   `auth.registration_enabled`.

::: warning Email matching trusts the provider
`auth.link.match_email` is off by default, and enabling it means trusting the
identity provider to own every address it asserts.

Local addresses are never verified by Voltis. Someone who sets another person's
address on their own account before that person first signs in receives the
link. Equally, anyone who can register a matching verified address at the
provider can claim the local account that uses it.
:::

## Losing access

Everything needed to recover is available from the CLI, which writes straight to
the database:

```bash
# Password login was disabled and the provider is unreachable
./voltis settings set auth.password_login_enabled true

# The last admin was demoted by a group mapping
./voltis users update myuser --admin
```

Public registration of the first admin closes for good once any admin exists,
whether that admin was created by registration, by the CLI or by a group
mapping, so this is the only way back in.

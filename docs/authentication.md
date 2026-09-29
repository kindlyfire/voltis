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

[OPDS apps](/opds) use per-user keys instead of these methods.

## Settings

| Key                              | Type   | Default                | Meaning                                                            |
| -------------------------------- | ------ | ---------------------- | ------------------------------------------------------------------ |
| `app.public_url`                 | string | _(empty)_              | Externally reachable base URL. Required for OIDC                   |
| `auth.registration_enabled`      | bool   | `false`                | Allow anyone to create an account                                  |
| `auth.password_login_enabled`    | bool   | `true`                 | Allow username and password login                                  |
| `auth.external_auto_create`      | bool   | `true`                 | Create an account on first OIDC or proxy login                     |
| `auth.external_session_max_days` | int    | `30`                   | Hard lifetime of a session created by OIDC or a proxy              |
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

::: warning
If `auth.admin_group` is set and the ID token carries no groups claim, Voltis
must reach `userinfo` to know whether the user is still an admin. When that
request fails the sign-in is refused.
:::

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
2. `auth.link.match_email`, if the address is verified and one account has it.
3. `auth.link.match_username`, if one account has that username. Through OIDC
   the user must confirm with that account's password, unless the account has
   no password and no identities yet — an account an admin pre-created for
   them. Through a proxy the link is automatic.
4. `auth.external_auto_create`: create a new account. This is not governed by
   `auth.registration_enabled`.

::: warning Email matching trusts the provider
`auth.link.match_email` is off by default, and enabling it means trusting the
identity provider.

Local addresses are never verified by Voltis. Anyone who can register a matching
verified address at the provider can claim the local account that uses it.
:::

## Losing access

Everything needed to recover is available from the CLI:

```bash
# Password login was disabled and the provider is unreachable
./voltis settings set auth.password_login_enabled true

# The last admin was demoted by a group mapping
./voltis users update myuser --admin
```

Public registration of the first admin closes for good once any admin exists,
whether that admin was created by registration, by the CLI or by a group
mapping, so this is the only way back in.

# Single sign-on with Authelia (forward-auth)

PgFleet can sit behind [Authelia](https://www.authelia.com/) so the dashboard is
protected by SSO + multi-factor auth, with built-in brute-force regulation —
before a request ever reaches the control plane. PgFleet still keeps its own
users, roles, and audit trail; it just trusts an identity the proxy has already
verified.

A ready-to-adapt stack lives in [`deploy/authelia/`](../deploy/authelia/).

## How it fits together

```
internet ──▶ Caddy (TLS + reverse proxy + forward_auth)
               │   unauthenticated? → redirect to the Authelia portal
               ├──▶ Authelia   login + TOTP MFA + brute-force regulation
               └──▶ PgFleet    (only after Authelia approves; Caddy injects the
                    verified Remote-Email / Remote-Groups headers)
                       │
                       └─ POST /api/v1/auth/sso  →  PgFleet session token
```

1. Caddy runs `forward_auth` against Authelia for every request. No valid
   session ⇒ the user is bounced to the Authelia portal to log in (with MFA).
2. Once authenticated, Authelia returns the user's identity; Caddy copies it onto
   the upstream request as `Remote-Email` and `Remote-Groups`.
3. The dashboard calls `POST /api/v1/auth/sso`. PgFleet reads the trusted header,
   finds (or, if enabled, provisions) the matching PgFleet user, and returns a
   normal PgFleet token — the same session it would issue for a password login.

## Why this stops credential cracking

A reverse proxy alone does **not** protect the dashboard — anyone who reaches the
hostname still hits the login form. Authelia closes that gap:

- **MFA (TOTP)** — a leaked/guessed password is not enough to get in.
- **Regulation** — after a few failed logins the account is banned for a cooldown
  (`regulation` in `configuration.yml`), defeating credential-stuffing.
- **Default-deny access control** — every route requires `two_factor` except the
  health probes.

PgFleet's own password login still exists as a fallback, but with SSO enabled you
typically expose only the proxy and keep the API unpublished.

## Enabling it on the PgFleet side

Set these on the API (see the compose file). SSO is mounted **only** when
`PGFLEET_SSO_EMAIL_HEADER` is set:

| Env var | Meaning | Example |
|---------|---------|---------|
| `PGFLEET_SSO_EMAIL_HEADER` | Header carrying the verified email | `Remote-Email` |
| `PGFLEET_SSO_GROUPS_HEADER` | Header carrying group membership | `Remote-Groups` |
| `PGFLEET_SSO_AUTO_PROVISION` | Create a PgFleet user on first SSO login | `true` |
| `PGFLEET_SSO_ADMIN_GROUP` | Group → PgFleet admin role | `pgfleet-admins` |
| `PGFLEET_SSO_OPERATOR_GROUP` | Group → PgFleet operator role | `pgfleet-operators` |

On the dashboard, set `NEXT_PUBLIC_OIDC_ENABLED=1` (and optionally
`NEXT_PUBLIC_OIDC_LABEL`) so the login page shows the SSO button. Auto-provisioned
users get the role mapped from their groups, defaulting to **viewer** (least
privilege) when no group matches.

## Security model — read this

The SSO endpoint **trusts the email header unconditionally**. That is only safe
because of two invariants you MUST uphold:

1. **The proxy strips client-supplied copies of the trusted headers.** The bundled
   `Caddyfile` does this with `header_up -Remote-Email` (and friends) *before*
   `forward_auth`, so the header can only be set from Authelia's verified
   response. If you use a different proxy, replicate this.
2. **The API is reachable only through the proxy.** Never publish the API port to
   the host or a public interface when SSO is on — a direct caller could set the
   header itself. In the compose file the API is `expose`d to the internal
   network only, never `ports:`-mapped.

Disabled PgFleet accounts are still rejected at the SSO exchange (403), so
off-boarding a user in PgFleet revokes dashboard access even if Authelia still
knows them.

## Swapping Authelia for another IdP

Any forward-auth/OIDC proxy that can inject a trusted email header works
(oauth2-proxy, Pomerium, Cloudflare Access with a header). Point
`PGFLEET_SSO_EMAIL_HEADER` at whatever header your proxy sets and keep the two
invariants above.

## First-run checklist

1. Replace every `change-me-*` secret in `deploy/authelia/docker-compose.yml`
   and the `REPLACE_ME` password hashes in `authelia/users_database.yml`
   (generate with `authelia crypto hash generate argon2`).
2. Set your real domain in `Caddyfile` and `authelia/configuration.yml`.
3. `docker compose -f deploy/authelia/docker-compose.yml up -d`.
4. Browse to your domain → Authelia portal → enroll TOTP → land on the PgFleet
   dashboard, already signed in.

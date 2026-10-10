# Security Policy

## Supported versions

Sherpa is pre-1.0. Security fixes go into the latest release and the `main` branch.

| Version | Supported |
| --- | --- |
| latest release / `main` | ✅ |
| older releases | ❌ |

## Reporting a vulnerability

Please **do not** open a public issue. Report privately through GitHub:
**Security → Report a vulnerability** on this repository
(<https://github.com/RL-Charles/sherpa/security/advisories/new>).

Include what you found, how to reproduce it and the impact you expect. You'll get an
acknowledgement within a few days; fixes are released as soon as practical and credited in the
release notes unless you'd rather stay anonymous.

## Scope notes

- `shp hooks install` writes hook configuration that coding agents execute. Report anything that
  could make those hooks run unintended commands.
- Sherpa calls `git` and the GitHub CLI (`gh`) with the user's own credentials; it does not store
  tokens itself.

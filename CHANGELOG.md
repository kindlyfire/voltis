# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

- OPDS 1.2 and 2.0 catalogs, with page streaming and progress sync for OPDS-PSE
  apps
- A scan removes nothing when a source folder lists empty or would lose most of
  its items, as when a drive is not mounted, and says why. The library setting
  "Remove missing items without checking" turns this off. It applies to scans
  queued after saving: an already queued scan keeps its settings
- Search matches titles and alternative titles. It matches every word except
  common short ones, allowing one miss in longer searches, and tolerates a
  typo. Exact titles rank first, then titles that start with the search
- Metadata, overrides and provider links are now deleted with their content,
  and the "Orphaned metadata" repair section is gone

### Upgrade notes

- Existing databases can't be upgraded. Start the
  `paradedb/paradedb:0.25.10-pg18` image on a fresh volume
- Setting `auth.admin_group` now demotes users whose OIDC login carries no
  groups. To restore an admin, fix the groups claim or clear the mapping with
  `voltis settings set auth.admin_group ""`, then run
  `voltis users update <name> --admin`. Without that, the next login demotes
  them again
- Forwarded auth no longer links accounts with a password by username or
  email. Link them with
  `voltis users link <name> --provider proxy --subject <name>`
- Email matching needs a verified email, and asks for the password of an
  account that has one

## [1.0.0-alpha.4] - 2026-09-24

- New UI
- New scanner
- Single sign-on (OIDC) and forwarded authentication

## [1.0.0-alpha.3] - 2026-03-29

- Added healthcheck to the Postgres container
- Tests now create temporary databases to run in isolation, and clean them up
  afterward
- New actions in the content multi-select menu: Scan, set reading status, reset
  reading progress
- Many task system updates
- Refactoring of content metadata handling code
- Requests to the MangaBaka API now include Voltis and the version in the user
  agent string

## [1.0.0-alpha.2] - 2026-03-15

- Easily add content to lists with multi-select in content grids
- Metadata: Allow linking content to MangaBaka manually. Link with search or a
  direct link through the metadata editor modal
- Metadata: Staff is now a "staff" array with name and role instead of
  individual fields for each role

## [1.0.0-alpha.1] - 2026-03-07

First alpha release. Detailed changelog entries will begin with future versions.

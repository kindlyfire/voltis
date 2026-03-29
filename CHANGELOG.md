# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

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

# Overview

Voltis is a self-hosted media server for [comics, manga, webtoons](/lib/comics)
and [ebooks](/lib/books).

It has:

- **Progress tracking:** Mark content as _reading_, _completed_, _on hold_,
  _dropped_ or _plan to read_. Track your reading position and resume where you
  left off
- **Search:** Search through your libraries, based on the series title
- **Custom lists:** Create public, private, or unlisted lists to organize your
  content. Reorder entries and add notes
- **Downloads:** Download individual chapters or books, or bundle an entire series as
  a ZIP. No offline reading in-app (yet)
- **Metadata:** Extracted from files during scanning, with support for
  [third-party metadata providers](/metadata) and manual overrides
- **Multi-user support:** Admin and regular user roles, with
  [single sign-on](/authentication) through OIDC or a reverse proxy
- **[OPDS](/opds):** Browse and read your libraries from OPDS reader apps

**[Installation](/installation)**

## Screenshots

<div style="display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px;">
  <Screenshot name="library_view" alt="Library view" />
  <Screenshot name="comic_view" alt="Comic detail" />
  <Screenshot name="reader_view" alt="Reader" />
  <Screenshot name="comic_update_progress" alt="Update progress" />
  <Screenshot name="settings_libraries" alt="Settings — Libraries" />
  <Screenshot name="settings_interface" alt="Settings — Interface" />
</div>

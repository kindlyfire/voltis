# Voltis

Voltis is a self-hosted media servers that supports comics/manga and ebooks. It
will support series, movies, and YouTube video libraries in the future as well.
I built it specifically to suit my needs. It is opinionated and may not work for
your needs.

I'm working on v1, a rewrite of v0 (dated early 2024) using Python and Vuetify.
v0 used Nuxt on client and server.

### To-do

Use this to get an idea of what can and can't be done. If you decide to use it
though, expect to reset your database often as things change. (Voltis does not
modify your files on disk.)

- [x] Extensible library scanning
  - [x] Library type: comics/manga
    - This already existed in v0 and has been improved further. Has paged and
      longstrip modes, action touch zones, automatically goes to the next chapter,
      but not many other options
  - [x] Library type: ebooks
    - Basic support is here, including reading, but needs polishing
  - [ ] Library type: series
  - [ ] Library type: movies
  - [ ] Library type: YT videos
  - [ ] Report scanning progress, allow cancelling
- [ ] Search
- [x] Overview/dashboard page
- [ ] User accounts
  - [x] Register/login
  - [x] Custom lists
  - [x] Ratings
  - [x] Reading statuses
  - [x] Progress tracking/resume reading
  - [ ] Reading history
  - [ ] Reading time tracking
- [ ] Admin dashboard
  - [x] Manage users
  - [x] Manage libraries
  - [x] Trigger library scans
  - [ ] Remap dead content links
    - Links to content, such as a users' rating, or list entries, are kept when
      the associated content is deleted. We'll allow users to remap their own,
      and admins to bulk remap
    - It already links by resolved content URI, so if you delete a file, scan,
      add the file again, scan again, the links will be restored automatically.
- [x] Dark mode
- [x] Mobile-friendly UI (mostly)

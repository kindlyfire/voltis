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
- [ ] Overview/dashboard page
- [ ] User accounts
  - [x] Register/login
  - [ ] Reading statuses
  - [ ] Custom lists
  - [ ] Ratings
  - [ ] Reading history
- [ ] Admin dashboard
  - [x] Manage users
  - [x] Manage libraries
  - [x] Trigger library scans
- [x] Dark mode
- [x] Mobile-friendly UI (mostly)

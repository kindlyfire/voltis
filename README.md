# Voltis

Voltis is a self-hosted media servers that supports comics/manga and ebooks. It
will support series, movies, and YouTube video libraries in the future as well.

**[Documentation](https://voltis.tijlvdb.me/)**

This repository is a mirror from
[git.tijlvdb.me](https://git.tijlvdb.me/tijlvdb/voltis), and does not accept
issues or pull requests at the moment.

## Roadmap

I will release alpha and beta versions of Voltis roughly in this order:

For 1.0.0-alpha.X:

- [ ] Feature-parity of the Go backend with the old Python backend
- [x] View scan history and logs in the frontend

For 1.0.0-beta.X:

- [ ] Tests for every endpoint + scanner
- [ ] Keep track of multiple reading tracks per content
- [ ] MangaBaka integration
- [ ] Scan files per folder and commit to database for each, instead of scanning
      the entire library at once
- [ ] Improved books support
- [ ] Track tags/genres/staff (based on MB tags v2)

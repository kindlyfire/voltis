# Comics

Voltis supports scanning comics in `.cbz` containers, and supports the
`ComicInfo.xml` metadata format.

The reader has paged (comics, manga) and longstrip modes.

## Filesystem layout

Chapters or volumes of a comic should be placed in a directory named after the
comic. We detect chapter and volume numbers based on:

- Any prefix of `Chapter` (C, Ch, Chap, ...) followed by a number for the
  chapter, case-insensitive, optionally with leading zeros, whitespace, or a
  dot.
- Any prefix of `Volume` (V, Vol, ...) with the same rules as above.

File names and series names are cleaned up prior to analysis: Groups of `[]`
and `()` are removed from the end of the name.

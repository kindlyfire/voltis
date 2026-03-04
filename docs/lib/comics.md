# Comics

Voltis supports scanning comics in `.cbz`, `.zip`, `.cbr`, `.rar` and `.pdf`
containers, and supports the `ComicInfo.xml` metadata format.

The reader has paged (comics, manga) and longstrip (webtoons) modes.

## Filesystem layout

Chapters or volumes of a comic should be placed in a directory named after the
comic. We detect chapter and volume numbers based on:

- `Number` and `Volume` fields in `ComicInfo.xml`, if they're numbers.
- Any prefix of `Chapter` (C, Ch, Chap, ...) followed by a number for the
  chapter, case-insensitive, optionally with leading zeros, whitespace, or a
  dot.
- Any prefix of `Volume` (V, Vol, ...) with the same rules as above.
- As a fallback, if neither chapter nor volume is detected, it will take the
  first number of the file name as the chapter number. We remove the common
  prefix between the parent folder name and the file name before looking for a
  number, to help avoid picking up numbers from the series name.
- Should no chapter/volume be found still, we will attempt to take the year if
  available and between parentheses (for example `(2022)`), or from the `Year`
  field in `ComicInfo.xml`.

File names and series names are cleaned up prior to analysis: Groups of `[]` and
`()`, with anything in between, are removed from the end of the name.

The series title will be inherited from the chapter/volume `Series` field in
`ComicInfo.xml` if available, otherwise it will take the cleaned up parent
folder name.

A custom cover image can be set by placing a `cover.jpg`, `cover.jpeg`,
`cover.png` or `cover.webp` file in the series directory.

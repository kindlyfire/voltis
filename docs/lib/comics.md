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
- Any prefix of `Volume` (V, Vol, ...) with the same rules as above, or `#`
  followed by a number.
- As a fallback, if neither chapter nor volume is detected, it will take the
  number with the most digits in the file name as the chapter number. We
  remove the common prefix between the parent folder name and the file name
  before looking for a number, to help avoid picking up numbers from the series
  name. This is skipped for files marked `SP` or `SPnn`.
- Should no chapter/volume be found still, the comic is added without a
  number, titled after what's left of its file name (`Series Artbook` becomes
  `Artbook`). If nothing is left, as with `Series (2022)`, it takes the
  four-digit year between parentheses from the file name.

File names and series names are cleaned up prior to analysis: Groups of `[]` and
`()`, with anything in between, are removed from the end of the name.

The series title will be inherited from the chapter/volume `Series` field in
`ComicInfo.xml` if available, otherwise it will take the cleaned up parent
folder name.

The series year comes from the `Year` field in `ComicInfo.xml`, or from a year
between parentheses in the parent folder name. Series with the same name but a
different year are kept separate, so `Series (2019)` and `Series (2022)` are two
series. Files with the same series name and year are grouped together even if
they are in different directories.

A custom cover image can be set by placing a `cover.jpg`, `cover.jpeg`,
`cover.png` or `cover.webp` file in the series directory. Otherwise the cover of
the first chapter/volume is used.

## Specials

A comic is marked special when its file name has an `SP`, `SPnn` or `Omake`
word, its `ComicInfo.xml` `Number` is `SP` (or `SP 2`), or its `Format` is
`Special`. Unnumbered comics in a series that also has numbered ones are
specials too.

Specials get a Special badge. Unnumbered ones are listed in a Specials tab on
the series page, ordered by SP number and then file name, and continue reading
skips them. Admins can set Special to Yes or No with "Edit metadata".

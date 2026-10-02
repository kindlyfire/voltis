# Books

Voltis supports scanning `.epub` files. It extracts metadata and the cover
image, and the reader has paged and scrolling modes, with one or two pages
side by side in paged mode.

## Filesystem layout

No specific layout is expected for books. Series are taken from the epub
metadata, using the EPUB 3 `belongs-to-collection` and `group-position` fields,
or Calibre's `calibre:series` and `calibre:series_index`.

Books without series metadata can be grouped by the volume number in their
title, or in their file name if the title has none. This is controlled by the
"Series without metadata" library setting, and is on by default. We look for
`Vol.`, `Volume` or `v` followed by a number, preceded by the series name, for
example `Series Vol. 3: Subtitle` or `Series v03 [Group]`. Groups of `[]`, `()`
and `{}` at the end of file names are ignored.

We don't infer a series when:

- The name contains a volume range (`v1-3`) or more than one volume number.
- It looks like a special: `SP01`, short or side stories, bonus, extra, or
  exclusive.
- The title and file name both have a volume number, and they differ.

A standalone book whose file name matches a series name is added to that
series. Changing the setting only regroups existing books on the next forced
scan.

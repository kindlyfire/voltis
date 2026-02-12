# Books

Book support exists, but is incomplete. Voltis can:

- Scan for `.epub` files
- Extract metadata and the cover image
- Read chapters, with image support

But it is incomplete in _at least_ the following ways:

- Chapter list includes many additional items that are not actually chapters,
  but rather parts of them. The chapter titles are also typically not correct
- Links inside of chapters to other chapters do not work

## Filesystem layout

No specific layout is expected for books, but if you want books belonging to a
series to be grouped together, you must set the series name appropriately in the
epub metadata. We do not do any automatic grouping based on filename or
directory structure.

/** The paged frame, shared by `BookReader.vue` and the browser tests. Each
 * slice is its own multicol, placed at its first column by the paginator, which
 * also writes the integer `--pg-*` geometry onto the viewport. */
export const PAGED_CSS = `
html.book-paged {
    overscroll-behavior: none;
}
.book-reader.is-paged {
    position: relative;
    height: calc(100dvh - var(--layout-top, 0px));
    min-height: 0;
    overflow: hidden;
}
.book-reader.is-paged .book-viewport {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    padding: var(--pg-pad-top, 0) env(safe-area-inset-right) 0 env(safe-area-inset-left);
}
.book-reader.is-paged .book-host {
    position: relative;
    flex: none;
    width: var(--pg-frame-w);
    height: var(--pg-h);
    overflow: hidden;
    overflow-anchor: none;
    font-size: var(--reader-font-size);
}
.book-reader.is-paged .book-host > .book-slice {
    position: absolute;
    top: 0;
    box-sizing: border-box;
    width: var(--pg-col-w);
    height: var(--pg-h);
    max-width: none;
    margin: 0;
    padding: 0;
    column-width: var(--pg-col-w);
    column-gap: var(--pg-gap);
    column-fill: auto;
}
.book-reader.is-paged .book-host > .book-slice.is-empty {
    display: none;
}
.book-reader.is-paged .book-host > .book-extender {
    position: absolute;
    top: 0;
    width: 1px;
    height: 1px;
}
@media print {
    .book-reader.is-paged {
        height: auto;
        overflow: visible;
    }
    .book-reader.is-paged .book-viewport,
    .book-reader.is-paged .book-host {
        display: block;
        width: auto;
        height: auto;
        overflow: visible;
        padding: 0;
    }
    .book-reader.is-paged .book-host > .book-slice {
        position: static;
        width: auto;
        height: auto;
        max-width: var(--reader-max-width);
        margin: 0 auto;
        padding: 2rem;
        columns: auto;
    }
    .book-reader.is-paged .book-footer,
    .book-reader.is-paged .book-extender {
        display: none;
    }
}
`

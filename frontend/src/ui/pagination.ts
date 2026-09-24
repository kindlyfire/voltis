export type PageItem = number | 'gap'

/**
 * Page numbers to show, compressed with gaps to at most 7 items: the first and last page,
 * and the current page with its neighbours.
 */
export function pageItems(page: number, length: number): PageItem[] {
    const range = (from: number, to: number) =>
        Array.from({ length: to - from + 1 }, (_, i) => from + i)
    if (length <= 7) return range(1, length)
    if (page <= 4) return [...range(1, 5), 'gap', length]
    if (page >= length - 3) return [1, 'gap', ...range(length - 4, length)]
    return [1, 'gap', page - 1, page, page + 1, 'gap', length]
}

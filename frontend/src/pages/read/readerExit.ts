export type ReaderExit = { to: string; label: string }

export function readerExit(
    parentId: string | null | undefined,
    contentId: string,
    itemLabel: string
): ReaderExit {
    return parentId
        ? { to: `/${parentId}`, label: 'Back to the series' }
        : { to: `/${contentId}`, label: itemLabel }
}

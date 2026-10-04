import type { DisplayMetadata } from '@/utils/api/metadata'
import type { ReadingDirection } from './types'

type Meta = Pick<DisplayMetadata, 'manga' | 'kind'> | null | undefined

// `Yes` and `Unknown` say nothing about page order: taggers write `Yes` for any manga.
function fromManga(manga: string | undefined): ReadingDirection | null {
    const value = manga?.toLowerCase()
    if (value === 'yesandrighttoleft') return 'rtl'
    if (value === 'no') return 'ltr'
    return null
}

/** The file's own tag beats the series' (a series can mix flipped and unflipped volumes); the
 * series kind is the weakest signal. */
export function detectDirection(comicMeta: Meta, seriesMeta: Meta): ReadingDirection {
    return (
        fromManga(comicMeta?.manga) ??
        fromManga(seriesMeta?.manga) ??
        (seriesMeta?.kind === 'manga' ? 'rtl' : 'ltr')
    )
}

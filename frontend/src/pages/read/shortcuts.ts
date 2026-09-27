import type { LayoutKind } from './BookDisplay/readingLayout'

export type ReaderKind = 'comic' | 'book'

export type Shortcut = [keys: string, action: string]

export const COMIC_SHORTCUTS: Shortcut[] = [
    ['Left arrow', 'Previous'],
    ['Right arrow', 'Next'],
    ['Comma', 'Previous Entry'],
    ['Period', 'Next Entry'],
    ['M', 'Show or hide the panel'],
]

export function bookShortcuts(mode: LayoutKind): Shortcut[] {
    const [forward, back] = mode === 'paged' ? ['Next page', 'Previous page'] : ['Forward', 'Back']
    return [
        ['Space, ↓, →, PgDn', forward],
        ['Shift+Space, ↑, ←, PgUp', back],
        ['Comma', 'Previous chapter'],
        ['Period', 'Next chapter'],
        ['M', 'Show or hide the panel'],
    ]
}

/** `M` toggles the reader's drawer. Presses with a modifier and key repeats are left alone. */
export function isDrawerToggle(e: KeyboardEvent): boolean {
    return (
        e.key.toLowerCase() === 'm' &&
        !e.repeat &&
        !e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey
    )
}

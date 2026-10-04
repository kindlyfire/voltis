import type { LayoutKind } from './BookDisplay/readingLayout'

export type ReaderKind = 'comic' | 'book'

export type Shortcut = [keys: string, action: string]

/** `flipped` swaps the left and right arrows, as in right-to-left reading. */
export function comicShortcuts(o: { flipped: boolean; paged: boolean }): Shortcut[] {
    const [left, right] = o.flipped ? ['Next', 'Previous'] : ['Previous', 'Next']
    return [
        ['Left arrow', left],
        ['Right arrow', right],
        ...(o.paged
            ? ([
                  ['Space, PgDn', 'Next'],
                  ['Shift+Space, PgUp', 'Previous'],
                  ['↑, ↓', 'Scroll'],
                  ['S', 'Shift spreads'],
              ] satisfies Shortcut[])
            : []),
        ['Comma', 'Previous Entry'],
        ['Period', 'Next Entry'],
        ['M', 'Show or hide the panel'],
    ]
}

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

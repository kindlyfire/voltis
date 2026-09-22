export type ReaderKind = 'comic' | 'book'

export type Shortcut = [keys: string, action: string]

export const COMIC_SHORTCUTS: Shortcut[] = [
    ['Left arrow', 'Previous'],
    ['Right arrow', 'Next'],
    ['Comma', 'Previous Entry'],
    ['Period', 'Next Entry'],
]

export const BOOK_SHORTCUTS: Shortcut[] = [
    ['Space, ↓, →, PgDn', 'Forward'],
    ['Shift+Space, ↑, ←, PgUp', 'Back'],
    ['Comma', 'Previous chapter'],
    ['Period', 'Next chapter'],
]

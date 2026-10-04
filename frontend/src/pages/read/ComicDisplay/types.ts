export interface PageDimensions {
    width: number
    height: number
}

export type ReaderMode = 'paged' | 'longstrip'
export type ReadingDirection = 'ltr' | 'rtl'
export type FitMode = 'screen' | 'width' | 'height'
export type SpreadSetting = 'single' | 'double' | 'auto'

/** The paged view's scroll API, registered with the store for the controls. */
export interface PagedScroller {
    /** True when it scrolled; false when the move should turn the page. */
    step(dir: 'next' | 'prev'): boolean
    /** Scrolls vertically by a fraction of the viewport height. */
    nudge(fraction: number): void
    /** Whether the viewport is at each physical x edge (both without horizontal overflow). */
    edges(): { left: boolean; right: boolean }
    /** Set by a turn right before the page changes; null means a placement. */
    enterAt: 'start' | 'end' | null
}

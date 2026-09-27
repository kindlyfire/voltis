import type { BookSpread } from './bookSettings'
import { shadowSelection } from './domSafe'
import { firstRect, pagedFrame, type VisibleFrame } from './locatorGeometry'
import { computeGeometry, type PagedGeometry } from './pagedGeometry'

export interface ScreenState {
    index: number
    count: number
    spread: 1 | 2
}

interface PaginatorHooks {
    spread(): BookSpread
    /** The frame moved to another screen without being told to: find-in-page,
     * focus, `scrollIntoView`, a selection drag. */
    onUserMove(): void
}

type ComposedSelection = Selection & {
    direction?: string
    getComposedRanges?(options: { shadowRoots: ShadowRoot[] }): StaticRange[]
}

function clamp(value: number, min: number, max: number) {
    return Math.min(max, Math.max(min, value))
}

/** Lays the host's slices out as one strip of columns, each slice its own
 * multicol starting on a fresh column, and shows it a screen at a time through
 * the host's `scrollLeft`, which nothing else may own. */
export function createPaginator(host: HTMLElement, screen: ScreenState, hooks: PaginatorHooks) {
    const viewport = host.parentElement!
    let geometry: PagedGeometry | null = null
    let extender: HTMLElement | null = null
    let expected = 0
    let pointerDown = false
    let draggedAway = false

    function slices(): HTMLElement[] {
        return (Array.from(host.children) as HTMLElement[]).filter(el =>
            el.classList.contains('book-slice')
        )
    }

    function roots(): ShadowRoot[] {
        return slices().flatMap(el => (el.shadowRoot ? [el.shadowRoot] : []))
    }

    function layout(): PagedGeometry {
        const textWidth = parseFloat(getComputedStyle(viewport).getPropertyValue('--reader-width'))
        const g = computeGeometry({
            width: viewport.clientWidth,
            height: viewport.clientHeight,
            fontPx: parseFloat(getComputedStyle(host).fontSize) || 16,
            textWidth: textWidth || 45,
            spread: hooks.spread(),
        })
        const vars: Record<string, number> = {
            '--pg-col-w': g.colW,
            '--pg-gap': g.gap,
            '--pg-h': g.pageH,
            '--reader-page-height': g.pageH,
            '--pg-frame-w': g.frameW,
            '--pg-pad-top': g.padTop,
        }
        for (const [name, value] of Object.entries(vars)) {
            viewport.style.setProperty(name, `${value}px`)
        }
        geometry = g
        screen.spread = g.spread
        return g
    }

    /** Counts from each slice's last fragment: `scrollWidth` over-counts
     * anything wider than a column. Each count is taken against the slice's
     * own box, so moving one slice can't skew the next. Clamps the screen
     * index without showing it. */
    function measure() {
        const g = geometry ?? layout()
        let start = 0
        for (const slice of slices()) {
            if (slice.classList.contains('is-empty')) continue
            slice.style.left = `${start * g.colPitch}px`
            const html = slice.shadowRoot?.querySelector('html')
            const rects = html
                ? Array.from(html.getClientRects()).filter(rect => rect.width || rect.height)
                : []
            if (!rects.length) continue
            const left = slice.getBoundingClientRect().left
            start += Math.floor((rects.at(-1)!.left - left + 0.5) / g.colPitch) + 1
        }
        screen.count = Math.max(1, Math.ceil(start / g.spread))
        // Makes the last screen of an odd column count reachable.
        if (!extender?.isConnected) {
            extender = document.createElement('div')
            extender.className = 'book-extender'
            host.append(extender)
        }
        extender.style.left = `${screen.count * g.screenPitch - 1}px`
        screen.index = clamp(screen.index, 0, screen.count - 1)
    }

    function showScreen(index: number) {
        const g = geometry ?? layout()
        screen.index = clamp(index, 0, screen.count - 1)
        // A selection drag keeps its own position until it ends.
        if (draggedAway) return
        expected = screen.index * g.screenPitch
        host.scrollLeft = expected
    }

    function screenOf(target: Element | Range): number | null {
        const rect = firstRect(target)
        if (!rect || !geometry) return null
        const offset = rect.left - host.getBoundingClientRect().left + host.scrollLeft
        const column = Math.floor((offset + 0.5) / geometry.colPitch)
        return clamp(Math.floor(column / geometry.spread), 0, screen.count - 1)
    }

    function frame(): VisibleFrame {
        const left = host.getBoundingClientRect().left + host.clientLeft
        return pagedFrame(left, left + host.clientWidth)
    }

    function hasSelection() {
        if (!(window.getSelection()?.isCollapsed ?? true)) return true
        return roots().some(root => !(shadowSelection(root)?.isCollapsed ?? true))
    }

    /** Where a selection drag ended, so the snap follows the reader's hand. */
    function selectionFocus(): Range | null {
        const selection = window.getSelection() as ComposedSelection | null
        const range = new Range()
        const composed = selection?.getComposedRanges?.({ shadowRoots: roots() })[0]
        if (composed && !composed.collapsed) {
            if (selection!.direction === 'backward') {
                range.setStart(composed.startContainer, composed.startOffset)
            } else {
                range.setStart(composed.endContainer, composed.endOffset)
            }
            return range
        }
        for (const root of roots()) {
            const found = shadowSelection(root)
            if (found?.focusNode && !found.isCollapsed) {
                range.setStart(found.focusNode, found.focusOffset)
                return range
            }
        }
        return null
    }

    function snap(target: number) {
        const index = screen.index
        showScreen(target)
        if (screen.index !== index) hooks.onUserMove()
    }

    function onScroll() {
        if (!geometry || Math.abs(host.scrollLeft - expected) < 1) return
        // A drag-select autoscrolls; following it screen by screen would jitter.
        if (pointerDown && hasSelection()) {
            draggedAway = true
            return
        }
        snap(Math.round(host.scrollLeft / geometry.screenPitch))
    }

    function onPointerDown() {
        pointerDown = true
    }

    function onPointerUp() {
        pointerDown = false
        if (!draggedAway || !geometry) return
        draggedAway = false
        const focus = selectionFocus()
        snap((focus && screenOf(focus)) ?? Math.round(host.scrollLeft / geometry.screenPitch))
    }

    host.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('pointerdown', onPointerDown, { capture: true, passive: true })
    window.addEventListener('pointerup', onPointerUp, { capture: true, passive: true })
    window.addEventListener('pointercancel', onPointerUp, { capture: true, passive: true })

    return {
        host,
        layout,
        measure,
        showScreen,
        screenOf,
        frame,
        dispose() {
            host.removeEventListener('scroll', onScroll)
            window.removeEventListener('pointerdown', onPointerDown, { capture: true })
            window.removeEventListener('pointerup', onPointerUp, { capture: true })
            window.removeEventListener('pointercancel', onPointerUp, { capture: true })
            extender?.remove()
            for (const slice of slices()) slice.style.left = ''
            host.scrollLeft = 0
        },
    }
}

export type Paginator = ReturnType<typeof createPaginator>

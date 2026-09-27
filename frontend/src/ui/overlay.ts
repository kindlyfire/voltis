import {
    computed,
    onScopeDispose,
    shallowRef,
    toValue,
    watch,
    type InjectionKey,
    type MaybeRefOrGetter,
} from 'vue'
import { hasOpenModal } from '@/utils/modals'

/** Provided by overlays that set initial focus themselves (ADialog, APopover) on their `[autofocus]` element. */
export const focusOwnerKey: InjectionKey<boolean> = Symbol('focusOwner')

/**
 * Open kit overlays, in opening order. Dialogs, menus and popovers take keyboard input while
 * open; drawers don't (the reader keeps its keys unless focus is inside the drawer).
 */
type Layer = { id: symbol; kind: 'drawer' | 'nav-drawer' | 'dialog' | 'menu' | 'popover' }
const layers = shallowRef<Layer[]>([])

// Visual stacking (the `--z-*` tokens): the main-nav drawer is above page drawers whatever the
// opening order, and dialogs, menus and popovers are above both. Same rank: the later one is on top.
const RANK: Record<Layer['kind'], number> = {
    drawer: 0,
    'nav-drawer': 1,
    dialog: 2,
    menu: 2,
    popover: 2,
}
const topLayer = computed(() =>
    layers.value.reduce<Layer | undefined>(
        (top, l) => (!top || RANK[l.kind] >= RANK[top.kind] ? l : top),
        undefined
    )
)

/** The main-nav drawer is open (it covers page drawers, so the reader leaves its drawer alone). */
export const navDrawerOpen = computed(() => layers.value.some(l => l.kind === 'nav-drawer'))

// While a drawer is open, the page doesn't scroll: the scrim covers it, but its scrollbar at the
// viewport edge would stay draggable. The gutter stays reserved so the page doesn't reflow, but
// only if there was a scrollbar: an unscrolled page would gain one. See ADrawer's global styles.
watch(
    () => layers.value.some(l => RANK[l.kind] < RANK.dialog),
    lock => {
        const html = document.documentElement
        // Measured before the lock hides the scrollbar.
        html.classList.toggle('drawer-lock-gutter', lock && innerWidth > html.clientWidth)
        html.classList.toggle('drawer-lock', lock)
    },
    { flush: 'sync' }
)

/** Registers an overlay while `open` is true. `isTop()` tells whether it's the topmost one. */
export function useOverlayLayer(kind: Layer['kind'], open: MaybeRefOrGetter<boolean>) {
    const id = Symbol(kind)
    const remove = () => (layers.value = layers.value.filter(l => l.id !== id))
    watch(
        () => toValue(open),
        isOpen => {
            remove()
            if (isOpen) layers.value = [...layers.value, { id, kind }]
        },
        { immediate: true }
    )
    onScopeDispose(remove)
    return { isTop: () => topLayer.value?.id === id }
}

// Focus inside these belongs to a control or overlay with its own key handling. A drawer panel
// itself isn't one: ADrawer focuses it on open, and the reader keeps its keys there.
const KEY_OWNERS = [
    '[data-reader-ignore-keys]',
    '[role="dialog"]',
    '[role="alertdialog"]',
    '[role="menu"]',
    '[role="listbox"]',
    '[role="slider"]',
    '[role="radiogroup"]',
    '[role="group"]',
].join(',')

/** A dialog, menu or popover is open (kit or imperative modal). Drawers don't count. */
export const overlayBlocking = computed(
    () => hasOpenModal.value || layers.value.some(l => RANK[l.kind] === RANK.dialog)
)

/**
 * Whether a global shortcut (the readers' keys) should leave this key alone: it was already
 * handled, an overlay that takes input is open, or focus is in a field, a control inside a
 * drawer, or a kit control with arrow-key behavior.
 */
export function keysOwnedElsewhere(e?: KeyboardEvent): boolean {
    if (e?.defaultPrevented || overlayBlocking.value) return true
    const active = document.activeElement
    if (!(active instanceof HTMLElement) || active === document.body) return false
    if (active.classList.contains('a-drawer')) return false
    return (
        active instanceof HTMLInputElement ||
        active instanceof HTMLTextAreaElement ||
        active instanceof HTMLSelectElement ||
        active.isContentEditable ||
        !!active.closest(KEY_OWNERS)
    )
}

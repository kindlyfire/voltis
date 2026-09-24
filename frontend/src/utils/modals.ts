import {
    type Component,
    computed,
    defineComponent,
    h,
    type InjectionKey,
    type PropType,
    provide,
    type Ref,
    ref,
    shallowRef,
} from 'vue'

/**
 * Handed to the ADialog a `Modals.show` target renders. The dialog calls `afterLeave` once its
 * leave transition is done: focus goes back and the entry is removed.
 */
export interface ModalEntryHooks {
    afterLeave(): void
}

export const modalEntryKey: InjectionKey<ModalEntryHooks | null> = Symbol('modalEntry')

interface ModalEntry {
    id: number
    component: Component
    props: Record<string, unknown>
    open: Ref<boolean>
    hooks: ModalEntryHooks
}

export interface ShowOptions {
    /** Where focus goes on close when the element that opened the modal is gone. */
    focusFallback?: () => HTMLElement | null | undefined
}

const entries = shallowRef<ModalEntry[]>([])
let nextId = 0

/** Checks `open` rather than the entry count: closed entries linger for their leave transition. */
export const hasOpenModal = computed(() => entries.value.some(e => e.open.value))

/** The element that opened a modal. A menu item unmounts with its menu, so use the menu's trigger. */
function captureOpener(): HTMLElement | null {
    const active = document.activeElement
    if (!(active instanceof HTMLElement) || active === document.body) return null
    const menu = active.closest('[role="menu"]')
    const trigger = menu && document.getElementById(menu.getAttribute('aria-labelledby') ?? '')
    return trigger ?? active
}

export const Modals = {
    /**
     * Renders `component` with `open` and `close` props. Its root must be an ADialog bound to
     * `open`, which ends the entry's lifecycle (see `ModalEntryHooks`).
     */
    show<T = void>(
        component: Component,
        props: Record<string, unknown> = {},
        options: ShowOptions = {}
    ): Promise<T> {
        return new Promise<T>(resolve => {
            const id = nextId++
            const open = ref(true)
            const opener = captureOpener()

            const hooks: ModalEntryHooks = {
                afterLeave() {
                    const target = opener?.isConnected
                        ? opener
                        : (options.focusFallback?.() ?? document.querySelector<HTMLElement>('main'))
                    target?.focus({ preventScroll: true })
                    entries.value = entries.value.filter(e => e.id !== id)
                },
            }

            const close = (data?: any) => {
                if (!open.value) return
                open.value = false
                resolve(data)
            }

            entries.value = [
                ...entries.value,
                { id, component, props: { ...props, close }, open, hooks },
            ]
        })
    },
}

const ModalEntryView = defineComponent({
    name: 'ModalEntryView',
    props: { entry: { type: Object as PropType<ModalEntry>, required: true } },
    setup(props) {
        provide(modalEntryKey, props.entry.hooks)
        return () =>
            h(props.entry.component, { ...props.entry.props, open: props.entry.open.value })
    },
})

export const ModalContainer = defineComponent({
    name: 'ModalContainer',
    setup() {
        return () => entries.value.map(entry => h(ModalEntryView, { key: entry.id, entry }))
    },
})

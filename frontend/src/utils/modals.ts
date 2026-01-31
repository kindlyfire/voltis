import { type Component, defineComponent, h, type Ref, ref, shallowRef } from 'vue'

interface ModalEntry {
    id: number
    component: Component
    props: Record<string, unknown>
    open: Ref<boolean>
    resolve: (value: any) => void
}

const entries = shallowRef<ModalEntry[]>([])
let nextId = 0

function removeEntry(id: number) {
    entries.value = entries.value.filter(e => e.id !== id)
}

export const Modals = {
    show<T = void>(component: Component, props: Record<string, unknown> = {}): Promise<T> {
        return new Promise<T>(resolve => {
            const id = nextId++
            const open = ref(true)

            const close = (data?: any) => {
                open.value = false
                resolve(data)
                setTimeout(() => {
                    removeEntry(id)
                }, 300)
            }

            const entry: ModalEntry = {
                id,
                component,
                props: { ...props, close },
                open,
                resolve,
            }

            entries.value = [...entries.value, entry]
        })
    },
}

export const ModalContainer = defineComponent({
    name: 'ModalContainer',
    setup() {
        return () =>
            entries.value.map(entry =>
                h(entry.component, {
                    key: entry.id,
                    ...entry.props,
                    open: entry.open.value,
                })
            )
    },
})

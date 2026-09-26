import { flushPromises } from '@vue/test-utils'
import { vi } from 'vitest'
import { defineComponent, h } from 'vue'
import ADialog from '@/ui/ADialog.vue'

/** A minimal `Modals.show` target. */
export const TestDialog = defineComponent({
    props: ['open', 'close'],
    setup: props => () =>
        h(ADialog, { open: props.open, title: 'Test', 'onUpdate:open': () => props.close() }, () =>
            h('button', 'Inside')
        ),
})

/** The portal target ADialog and the overlay components render into. */
export function addOverlays() {
    const overlays = document.createElement('div')
    overlays.id = 'overlays'
    document.body.append(overlays)
}

// Reka's focus scope hands focus back in a timeout after the content unmounts.
export async function settle() {
    await flushPromises()
    await vi.runAllTimersAsync()
    await flushPromises()
}

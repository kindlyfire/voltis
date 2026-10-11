import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { hasOpenModal, ModalContainer, Modals, type ShowOptions } from '@/utils/modals'
import { addOverlays, settle, TestDialog } from '@/utils/modalTesting'
import { useReaderTutorial } from './useReaderTutorial'

const mutate = vi.fn()

vi.mock('@/utils/api/users', () => ({
    usersApi: {
        useMe: () => ({ data: ref({ preferences: { tutorials: {} } }) }),
        usePatchPreferences: () => ({ mutate }),
    },
}))

vi.mock('./ReaderTutorialModal.vue', () => ({
    showReaderTutorial: (_kind: string, _mode: unknown, _flipped: unknown, options: ShowOptions) =>
        Modals.show(TestDialog, {}, options),
}))

beforeEach(() => {
    vi.useFakeTimers()
    addOverlays()
})

afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
    mutate.mockClear()
})

const Host = defineComponent({
    setup() {
        useReaderTutorial('comic', ref(true))
        return () => null
    },
})

it('closes without saving when the reader unmounts, and saves once confirmed', async () => {
    const container = mount(ModalContainer, { attachTo: document.body })

    const left = mount(Host)
    await settle()
    expect(hasOpenModal.value).toBe(true)
    left.unmount()
    await settle()
    expect(hasOpenModal.value).toBe(false)
    expect(mutate).not.toHaveBeenCalled()

    const stayed = mount(Host)
    await settle()
    container.findComponent(TestDialog).props('close')()
    await settle()
    expect(mutate).toHaveBeenCalledWith({ tutorials: { comicReader: true } })
    stayed.unmount()
    container.unmount()
})

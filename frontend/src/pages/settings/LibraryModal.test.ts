import { VueQueryPlugin } from '@tanstack/vue-query'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import LibraryModal from '@/pages/settings/LibraryModal.vue'
import type { Library } from '@/utils/api/types'

const libraries = ref<Library[]>()

vi.mock('@/utils/api/libraries', () => ({
    librariesApi: {
        useList: () => ({ data: libraries }),
        useUpsert: () => ({ mutateAsync: vi.fn(), isPending: ref(false) }),
        useDelete: () => ({ mutateAsync: vi.fn(), isPending: ref(false), isError: ref(false) }),
    },
}))

vi.mock('@/utils/api/fs', () => ({
    fsApi: { resolve: async (paths: string[]) => paths.map(p => ({ input: p, path: p })) },
}))

const stubs = {
    ADialog: { template: '<div><slot /><slot name="actions" /></div>' },
    ATooltip: { template: '<slot />' },
    QueryError: { template: '<div />' },
}

function library(name: string): Library {
    return {
        id: 'l_1',
        created_at: '',
        updated_at: '',
        name,
        type: 'comics',
        content_count: 0,
        root_content_count: 0,
        scanned_at: null,
        sources: [{ path_uri: '/comics' }],
        settings: { book_series_inference: 'conservative', auto_match: true },
    }
}

enableAutoUnmount(afterEach)

describe('LibraryModal', () => {
    it('keeps unsaved edits when the libraries refetch', async () => {
        libraries.value = undefined
        const wrapper = mount(LibraryModal, {
            props: { open: true, close: () => {}, libraryId: 'l_1' },
            global: { stubs, plugins: [VueQueryPlugin] },
        })
        libraries.value = [library('Comics')]
        await flushPromises()
        const name = wrapper.find<HTMLInputElement>('input')
        expect(name.element.value).toBe('Comics')

        await name.setValue('Edited')
        libraries.value = [library('Renamed elsewhere')]
        await flushPromises()
        expect(name.element.value).toBe('Edited')
    })
})

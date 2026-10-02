import { VueQueryPlugin } from '@tanstack/vue-query'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { computed, ref } from 'vue'
import LibraryModal from '@/pages/settings/LibraryModal.vue'
import ASelect from '@/ui/ASelect.vue'
import type { MetadataConfig } from '@/utils/api/metadata'
import type { Library, LibraryUpsert } from '@/utils/api/types'
import { addOverlays } from '@/utils/modalTesting'

const libraries = ref<Library[]>()
const config = ref<MetadataConfig>()
const configError = ref<Error>()
const upserted: LibraryUpsert[] = []

vi.mock('@/utils/api/libraries', () => ({
    librariesApi: {
        useList: () => ({ data: libraries }),
        useUpsert: () => ({
            mutateAsync: async (body: LibraryUpsert) => {
                upserted.push(body)
                return { ...library(body.name), id: 'l_new' }
            },
            isPending: ref(false),
        }),
        useDelete: () => ({
            mutateAsync: vi.fn(),
            isPending: ref(false),
            isError: ref(false),
            error: ref(null),
        }),
    },
}))

vi.mock('@/utils/api/metadata', () => ({
    metadataApi: {
        useConfig: () => ({
            data: config,
            isPending: computed(() => !config.value && !configError.value),
            isError: computed(() => !!configError.value),
            error: configError,
        }),
    },
}))

vi.mock('@/utils/api/fs', () => ({
    fsApi: { resolve: async (paths: string[]) => paths.map(p => ({ input: p, path: p })) },
}))

const stubs = {
    ADialog: { template: '<div><slot /><slot name="actions" /></div>' },
    ATooltip: { template: '<slot />' },
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
        sources: [
            { path_uri: '/comics', settings: {} },
            {
                path_uri: '/comics/western',
                settings: { auto_match: { mangabaka: false, gone: true } },
            },
        ],
        settings: {
            book_series_inference: 'conservative',
            auto_match: { mangabaka: true, gone: true },
            always_remove_missing: false,
        },
    }
}

beforeEach(() => {
    addOverlays()
    libraries.value = [library('Comics')]
    config.value = {
        fields: [],
        providers: [
            { name: 'mangabaka', label: 'MangaBaka', content_types: ['comic_series'] },
            { name: 'books', label: 'Books DB', content_types: ['book_series'] },
        ],
    }
    configError.value = undefined
    upserted.length = 0
})

enableAutoUnmount(afterEach)

async function open(libraryId = 'l_1', close: (createdId?: string) => void = () => {}) {
    const wrapper = mount(LibraryModal, {
        props: { open: true, close, libraryId },
        global: { stubs, plugins: [VueQueryPlugin] },
    })
    await flushPromises()
    const tab = (name: string) => wrapper.findAll('[role="tab"]').find(t => t.text() === name)!
    const panel = (name: string) => {
        const id = tab(name).attributes('aria-controls')
        return wrapper.find(`[id="${id}"]`)
    }
    const button = (label: string) =>
        wrapper
            .findAll('button')
            .find(b => b.attributes('aria-label') === label || b.text() === label)!
    const submit = async () => {
        await wrapper.find('form').trigger('submit')
        await flushPromises()
    }
    return { wrapper, tab, panel, button, submit }
}

describe('LibraryModal', () => {
    it('keeps unsaved edits when the libraries refetch', async () => {
        libraries.value = undefined
        const { wrapper } = await open()
        libraries.value = [library('Comics')]
        await flushPromises()
        const name = wrapper.find<HTMLInputElement>('input')
        expect(name.element.value).toBe('Comics')

        await name.setValue('Edited')
        libraries.value = [library('Renamed elsewhere')]
        await flushPromises()
        expect(name.element.value).toBe('Edited')
    })

    it('shows a switch per provider of the type, and overrides as chips', async () => {
        const { tab, panel, button } = await open()
        expect(tab('General').attributes('aria-selected')).toBe('true')
        const switches = panel('General').find('[aria-busy]').findAll('input[role="switch"]')
        expect(switches.map(s => (s.element as HTMLInputElement).checked)).toEqual([true])
        expect(panel('General').text()).toContain('with MangaBaka')
        expect(panel('General').text()).not.toContain('Books DB')
        expect(panel('Sources').text()).toContain('MangaBaka: off')
        expect(panel('Sources').text()).not.toContain('gone') // not registered: kept, not shown

        await button('Overridden in 1 source').trigger('click')
        expect(tab('Sources').attributes('aria-selected')).toBe('true')
        expect(button('Settings of source 2').attributes('aria-expanded')).toBe('true')
        expect(button('Settings of source 1').attributes('aria-expanded')).toBe('false')
    })

    it('warns on the sources tab without sources, and marks sources with overrides', async () => {
        const { wrapper, tab, button } = await open()
        const warning = () => tab('Sources').find('[aria-label="No sources"]').exists()
        expect(warning()).toBe(false)
        expect(wrapper.text()).not.toContain('No sources')
        expect(button('Save').attributes('disabled')).toBeUndefined()
        expect(button('Settings of source 1').classes()).toContain('variant-standard')
        expect(button('Settings of source 2').classes()).toContain('variant-tonal')

        for (const input of wrapper.findAll('input[placeholder="/path/to/folder"]')) {
            await input.setValue(' ')
        }
        expect(warning()).toBe(true)
        expect(wrapper.text()).toContain('No sources')
        expect(button('Save').attributes('disabled')).toBeDefined()
    })

    it('creates a library only with a source, and closes with its id', async () => {
        const close = vi.fn()
        const { wrapper, button, submit } = await open('new', close)
        expect(wrapper.text()).toContain('No sources')
        expect(button('Create and scan').attributes('disabled')).toBeDefined()

        await wrapper.find('input').setValue('Comics')
        wrapper.findComponent(ASelect).vm.$emit('update:modelValue', 'comics')
        await button('Add path manually').trigger('click')
        await wrapper.find('input[placeholder="/path/to/folder"]').setValue('/comics')
        expect(wrapper.text()).not.toContain('No sources')
        expect(button('Create and scan').attributes('disabled')).toBeUndefined()

        await submit()
        expect(close).toHaveBeenCalledWith('l_new')
    })

    it('labels inherit after the unsaved library value and hints when off', async () => {
        const { wrapper, panel, button } = await open()
        await button('Settings of source 1').trigger('click')
        const sources = panel('Sources')
        expect(sources.text()).toContain('Inherit (on)')
        const hints = () => sources.findAll('p').filter(p => p.text().includes('still matched'))
        expect(hints()).toHaveLength(0)
        await button('Settings of source 2').trigger('click')
        expect(hints()).toHaveLength(1)

        await panel('General').find('[aria-busy] input[role="switch"]').setValue(false)
        expect(sources.text()).toContain('Inherit (off)')
        expect(hints()).toHaveLength(2)
        expect(wrapper.text()).not.toContain('Inherit (on)')
    })

    it('sends sources without their keys, inherit left out, and settings kept by path edits', async () => {
        const { wrapper, panel, button, submit } = await open()
        const before = JSON.parse(JSON.stringify(libraries.value![0]))
        await button('Settings of source 1').trigger('click')
        await button('Settings of source 2').trigger('click')
        const groups = panel('Sources').findAll('[role="group"]')
        const choose = (group: number, label: string) =>
            groups[group]!.findAll('button')
                .find(b => b.text().startsWith(label))!
                .trigger('click')
        await choose(0, 'Off')
        await choose(1, 'Inherit')
        await choose(1, 'On')
        await wrapper
            .findAll<HTMLInputElement>('input[placeholder="/path/to/folder"]')[1]!
            .setValue('/manga')
        expect(libraries.value![0]).toEqual(before)

        await submit()
        expect(upserted).toEqual([
            {
                id: 'l_1',
                name: 'Comics',
                type: 'comics',
                sources: [
                    { path_uri: '/comics', settings: { auto_match: { mangabaka: false } } },
                    {
                        path_uri: '/manga',
                        settings: { auto_match: { mangabaka: true, gone: true } },
                    },
                ],
                settings: {
                    book_series_inference: 'conservative',
                    auto_match: { mangabaka: true, gone: true },
                    always_remove_missing: false,
                },
            },
        ])
        await choose(0, 'Inherit')
        await submit()
        expect(upserted[1]!.sources[0]).toEqual({
            path_uri: '/comics',
            settings: { auto_match: {} },
        })
    })

    it('switches to the tab of an invalid field', async () => {
        const { wrapper, tab, submit } = await open()
        await tab('Sources').trigger('mousedown')
        await tab('Sources').trigger('click')
        await flushPromises()
        expect(tab('Sources').attributes('aria-selected')).toBe('true')
        await wrapper.find<HTMLInputElement>('input').setValue('')
        await submit()
        expect(upserted).toEqual([])
        expect(tab('General').attributes('aria-selected')).toBe('true')
    })

    it('shows a placeholder while the providers load', async () => {
        config.value = undefined
        const { panel } = await open()
        expect(panel('General').find('[aria-busy="true"]').exists()).toBe(true)
        expect(panel('General').find('[aria-busy] input[role="switch"]').exists()).toBe(false)
    })

    it('shows why the providers failed to load, without a placeholder', async () => {
        config.value = undefined
        configError.value = new Error('config is down')
        const { panel, button } = await open()
        expect(panel('General').text()).toContain('config is down')
        expect(panel('General').find('[aria-busy="true"]').exists()).toBe(false)
        await button('Settings of source 1').trigger('click')
        expect(panel('Sources').text()).not.toContain('No metadata provider')
    })
})

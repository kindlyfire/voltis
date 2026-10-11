import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import { defineComponent } from 'vue'
import ASegmented from '@/ui/ASegmented.vue'
import type { BrokenRefGroup, BrokenRefPair, BrokenUserToContent } from '@/utils/api/types'
import BrokenRefMove from './BrokenRefMove.vue'

const row = (uri: string, status: string) =>
    ({ id: uri, uri, status, progress: {}, last_read_at: null }) as unknown as BrokenUserToContent

const group: BrokenRefGroup = {
    uri: 'comic/Old',
    items: ['ch1', 'ch2', 'ch3', 'ch4'].map(p => row(`comic/Old/${p}`, 'reading')),
    list_entries: [],
    suggestions: [],
}

const pair = (from: string, to: string, more: Partial<BrokenRefPair> = {}): BrokenRefPair => ({
    from: `comic/Old/${from}`,
    to: `comic/New/${to}`,
    by: 'exact',
    existing: null,
    list_merges: 0,
    ...more,
})

const Combobox = defineComponent({
    name: 'ACombobox',
    props: { label: String, modelValue: String, options: Array },
    emits: ['update:model-value'],
    template: '<i />',
})

function render(pairs: BrokenRefPair[], overrides: Record<string, string | null> = {}) {
    return mount(BrokenRefMove, {
        props: {
            group,
            preview: {
                data: {
                    pairs,
                    leftovers: ['comic/Old/ch3', 'comic/Old/ch4'],
                    free_targets: ['comic/New/ch3a'],
                },
                isError: false,
                error: null,
            },
            keep: 'newer',
            overrides,
            drop: new Map(),
            contentUris: ['comic/New', 'comic/New/ch1', 'comic/New/v1_ch2', 'comic/New/ch3a'],
        },
        global: {
            stubs: {
                ACombobox: Combobox,
                // Its tooltip needs a provider.
                AIconButton: { props: ['label'], template: '<button :aria-label="label" />' },
            },
        },
    })
}

it('sums up the preview, and shows the Keep choice only with conflicts', () => {
    const plain = render([pair('ch1', 'ch1', { list_merges: 1 }), pair('ch2', 'v1_ch2')])
    expect(plain.text()).toContain('2 of 4 items match · 2 left over · 1 list entry merges')
    expect(plain.findComponent(ASegmented).exists()).toBe(false)
})

it('shows conflicts, and repoints, unpairs, resets and deletes items', async () => {
    const wrapper = render(
        [
            pair('ch1', 'ch1', { existing: row('comic/New/ch1', 'completed') }),
            pair('ch2', 'v1_ch2'),
        ],
        { 'comic/Old/ch4': null }
    )
    expect(wrapper.text()).toContain('· 1 already has your data')
    expect(wrapper.findComponent(ASegmented).exists()).toBe(true)
    await wrapper
        .findAll('button')
        .find(b => b.text() === 'Details')!
        .trigger('click')
    expect(wrapper.text()).toContain('This:Reading')
    expect(wrapper.text()).toContain('Existing:Completed')

    const box = (src: string) =>
        wrapper
            .findAllComponents(Combobox)
            .find(c => c.props('label') === `Destination for comic/Old/${src}`)!
    const options = box('ch3').props('options') as { value: string; disabled: boolean }[]
    expect(options[0]).toEqual({
        value: 'comic/New/ch3a',
        label: 'comic/New/ch3a',
        disabled: false,
    })
    expect(options.find(o => o.value === 'comic/New/ch1')?.disabled).toBe(true)

    box('ch3').vm.$emit('update:model-value', 'comic/New/ch3a')
    box('ch1').vm.$emit('update:model-value', null)
    await wrapper
        .find('button[aria-label="Reset comic/Old/ch4 to automatic pairing"]')
        .trigger('click')
    await wrapper.find('button[aria-label="Delete the data for comic/Old/ch4"]').trigger('click')
    expect(wrapper.emitted('update:override')).toEqual([
        ['comic/Old/ch3', 'comic/New/ch3a'],
        ['comic/Old/ch1', null],
        ['comic/Old/ch4', undefined],
    ])
    expect(wrapper.emitted('toggle-drop')).toEqual([['comic/Old/ch4']])
})

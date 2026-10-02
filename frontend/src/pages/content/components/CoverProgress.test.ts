import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import CoverProgress from './CoverProgress.vue'

function mountWith(props: Partial<Content>, earlier: string | null = null) {
    const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
    queryClient.setQueryData(['content', 'c_1', 'continue'], { earlier_unread_id: earlier })
    const content = { id: 'c_1', type: 'book_series', ...props } as Content
    return shallowMount(CoverProgress, {
        props: { content },
        global: {
            plugins: [[VueQueryPlugin, { queryClient }]],
            stubs: {
                RouterLink: { template: '<a><slot /></a>' },
                AChip: { template: '<span><slot /></span>' },
            },
        },
    })
}

describe('CoverProgress', () => {
    it('shows a caught-up series in full, with its earlier volumes', () => {
        const w = mountWith(
            {
                children_count: 12,
                unread_children_count: 0,
                completed_children_count: 11,
                dropped_children_count: 1,
                user_data: { status: 'reading' } as Content['user_data'],
            },
            'c_0'
        )
        expect(w.text()).toContain('11/12 read · 1 dropped · Caught up')
        expect(w.text()).toContain('Earlier unread volumes')
    })

    it('counts new volumes of a completed series', () => {
        const w = mountWith({
            children_count: 5,
            unread_children_count: 2,
            completed_children_count: 3,
            new_children_count: 2,
            user_data: { status: 'completed' } as Content['user_data'],
        })
        expect(w.text()).toContain('2 new')
        expect(w.text()).toContain('3/5 read')
        expect(w.text()).not.toContain('Caught up')

        // Without any volume read, the count still shows.
        const none = mountWith({
            children_count: 1,
            unread_children_count: 1,
            completed_children_count: 0,
            new_children_count: 1,
            user_data: { status: 'completed' } as Content['user_data'],
        })
        expect(none.text()).toBe('1 new')
    })
})

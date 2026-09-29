import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { toasts } from '@/ui/useToast'
import { contentApi, invalidateContentWindow } from '@/utils/api/content'
import { useGridSelection } from './useGridSelection'

vi.mock('@/utils/api/content', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/api/content')>()),
    invalidateContentWindow: vi.fn(),
}))

describe('useGridSelection', () => {
    it('adds shift ranges, toggles only the clicked id on drift, and keeps the selection on error', async () => {
        const ids = vi
            .spyOn(contentApi, 'ids')
            .mockResolvedValueOnce({ ids: ['a', 'b', 'c', 'd'] })
            .mockResolvedValueOnce({ ids: ['b', 'z', 'y'] })
            .mockRejectedValueOnce(new Error('offline'))
        const s = useGridSelection(ref({}))
        const selected = () => [...s.ids.value].sort()

        await s.toggle('d', 3, false)
        await s.toggle('a', 0, true)
        expect(ids).toHaveBeenLastCalledWith({}, 0, 4)
        expect(selected()).toEqual(['a', 'b', 'c', 'd'])

        await s.toggle('b', 1, false)
        expect(selected()).toEqual(['a', 'c', 'd'])

        await s.toggle('f', 5, true)
        expect(selected()).toEqual(['a', 'c', 'd', 'f'])
        expect(invalidateContentWindow).toHaveBeenCalledOnce()

        await s.toggle('h', 7, true)
        expect(selected()).toEqual(['a', 'c', 'd', 'f'])
        expect(toasts.value.at(-1)).toMatchObject({ tone: 'danger' })
    })
})

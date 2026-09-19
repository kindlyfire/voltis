import { describe, expect, it, vi } from 'vitest'
import '@/utils/api/users'
import { queryClient } from '@/utils/misc'
import { ws } from '@/utils/ws'

vi.mock('@/utils/ws', () => {
    const handlers = new Map<string, Set<(msg: any) => void>>()
    return {
        ws: {
            connect: () => {},
            send: () => {},
            on(type: string, handler: (msg: any) => void) {
                let set = handlers.get(type)
                if (!set) handlers.set(type, (set = new Set()))
                set.add(handler)
                return () => set!.delete(handler)
            },
            emit(type: string, msg: any) {
                for (const handler of [...(handlers.get(type) ?? [])]) handler(msg)
            },
        },
    }
})

const emit = (ws as unknown as { emit: (type: string, msg: any) => void }).emit

describe('current user revalidation', () => {
    it.each(['$open', '$close'])('revalidates the current user on %s', type => {
        const spy = vi.spyOn(queryClient, 'invalidateQueries').mockImplementation(async () => {})

        emit(type, { type })
        expect(spy).toHaveBeenCalledWith({ queryKey: ['users', 'me'] })
    })
})

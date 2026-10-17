import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import {
    invalidateRecentScrobbles,
    invalidateScrobbleAccount,
    scrobbleApi,
} from '@/utils/api/scrobble'
import type { ScrobbleRow } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { ws } from '@/utils/ws'

/** The user's failed, pending and held scrobbles, kept current over the WebSocket. */
export const useScrobbleStore = defineStore('scrobbles', () => {
    const rows = ref<ScrobbleRow[]>([])
    const loaded = ref(false)

    const byId = computed(() => new Map(rows.value.map(r => [r.content_id, r])))
    const pending = computed(() => rows.value.filter(r => r.state === 'pending'))
    const failed = computed(() => rows.value.filter(r => r.state === 'failed'))
    const held = computed(() => rows.value.filter(r => r.state === 'held'))
    const visible = computed(() => pending.value.length > 0 || failed.value.length > 0)

    const row = (id: string) => byId.value.get(id)
    const isHeld = (id: string) => byId.value.get(id)?.state === 'held'

    let generation = 0
    let inFlight: Promise<void> | null = null
    let queued: Promise<void> | null = null

    async function load(): Promise<void> {
        const gen = generation
        try {
            const res = await scrobbleApi.list()
            if (gen !== generation) return
            rows.value = res
            loaded.value = true
        } catch (err) {
            console.error('Scrobble queue request failed', err)
        }
    }

    /** One request in flight and at most one queued behind it, so the last response is current. */
    function refresh(): Promise<void> {
        if (queued) return queued
        if (!inFlight) {
            inFlight = load().finally(() => (inFlight = null))
            return inFlight
        }
        queued = inFlight.then(() => {
            queued = null
            return refresh()
        })
        return queued
    }

    /** Sends now: every pending series, or `ids` with failed ones retried. */
    async function send(ids?: string[]): Promise<void> {
        await scrobbleApi.send(ids)
        await refresh()
    }

    function reset(): void {
        generation++
        rows.value = []
        loaded.value = false
    }

    return { rows, loaded, pending, failed, held, visible, row, isHeld, refresh, send, reset }
})

export function useScrobbleSync(): void {
    const store = useScrobbleStore()
    const qMe = usersApi.useMe()

    watch(
        () => qMe.data.value?.id,
        (id, _, onCleanup) => {
            store.reset()
            if (!id) return
            // Not the cached series dialogs' queries: each would read MangaBaka live on every push. A mounted
            // dialog refetches itself.
            const onChange = () => {
                void store.refresh()
                void invalidateScrobbleAccount()
                void invalidateRecentScrobbles()
            }
            const unsubs = [ws.on('scrobbles_changed', onChange), ws.on('$open', onChange)]
            onCleanup(() => unsubs.forEach(unsub => unsub()))
            void store.refresh()
        },
        { immediate: true }
    )
}

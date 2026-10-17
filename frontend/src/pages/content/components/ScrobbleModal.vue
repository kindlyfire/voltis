<template>
    <ADialog :open="open" title="Scrobbling" @update:open="v => !v && close()">
        <div v-if="q.isLoading.value" class="flex justify-center py-6">
            <ASpinner />
        </div>
        <QueryError :query="q" />
        <template v-if="data">
            <p v-if="data.available === 'not_connected'" class="text-sm">
                <RouterLink
                    :to="{ name: 'settings-scrobbling' }"
                    class="a-focus text-primary rounded-field font-medium hover:underline"
                    @click="close()"
                >
                    Connect MangaBaka in Settings
                </RouterLink>
                to scrobble.
            </p>
            <p v-else-if="data.available === 'library_off'" class="text-sm">
                Scrobbling is off for this library.
            </p>
            <p v-else-if="data.available === 'not_linked'" class="text-sm">
                Not linked to MangaBaka.
            </p>
            <div v-else class="flex flex-col gap-4">
                <ATable density="compact" class="-mx-3 max-w-none">
                    <thead>
                        <tr>
                            <th><span class="sr-only">Field</span></th>
                            <th>Voltis</th>
                            <th>MangaBaka</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-for="f in FIELDS" :key="f.key">
                            <th scope="row" class="text-fg-muted font-normal">{{ f.label }}</th>
                            <td>{{ show(data.ours[f.key]) }}</td>
                            <td v-if="data.remote_error && f.key === 'chapter'" rowspan="3">
                                <span class="text-fg-muted">{{ data.remote_error }}</span>
                            </td>
                            <td
                                v-else-if="!data.remote_error"
                                :class="{ 'text-warning font-semibold': ahead.includes(f.key) }"
                            >
                                {{ show(data.remote?.[f.key]) }}
                            </td>
                        </tr>
                    </tbody>
                </ATable>

                <AAlert v-if="data.linked.length" tone="warning">
                    Also linked to this MangaBaka entry:
                    <template v-for="(l, i) in data.linked" :key="l.content_id"
                        ><RouterLink
                            :to="{ name: 'info-page', params: { id: l.content_id } }"
                            class="a-focus text-primary rounded-field font-medium hover:underline"
                            @click="close()"
                            >{{ l.title }}</RouterLink
                        ><template v-if="l.held"> (on hold)</template
                        >{{ i < data.linked.length - 1 ? ', ' : '.' }}</template
                    >
                    Progress is combined{{
                        held || data.linked.some(l => l.held) ? ', except for series on hold' : ''
                    }}.
                </AAlert>
                <AAlert v-if="held" tone="info">On hold. Nothing is sent for this series.</AAlert>
                <template v-else>
                    <AAlert v-if="live.error" tone="danger">Failed: {{ live.error }}</AAlert>
                    <AAlert v-if="ahead.length" tone="warning">{{ aheadMessage }}</AAlert>
                    <p v-if="status" class="text-fg-muted text-sm">{{ status }}</p>
                </template>
                <QueryError :mutation="mHold" />
                <QueryError :mutation="mSend" />
            </div>
        </template>

        <template #actions>
            <template v-if="data?.available === 'ok'">
                <AButton
                    variant="text"
                    tone="neutral"
                    class="mr-auto"
                    :loading="mHold.isPending.value"
                    @click="mHold.mutate(!held)"
                >
                    {{ held ? 'Release' : 'Hold' }}
                </AButton>
                <template v-if="!held">
                    <AButton
                        v-if="ahead.length"
                        variant="text"
                        :loading="busy && !!mSend.variables.value?.force"
                        :disabled="busy"
                        focusable-when-disabled
                        @click="send(true)"
                    >
                        Overwrite MangaBaka
                    </AButton>
                    <AButton
                        variant="tonal"
                        :loading="busy && !mSend.variables.value?.force"
                        :disabled="busy"
                        focusable-when-disabled
                        @click="send(false)"
                    >
                        {{ live.error ? 'Retry' : 'Send now' }}
                    </AButton>
                </template>
            </template>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useNow } from '@vueuse/core'
import { capitalize, computed, onUnmounted, ref, watch, watchEffect } from 'vue'
import { RouterLink } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import { useScrobbleStore } from '@/stores/scrobbles'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import { scrobbleApi } from '@/utils/api/scrobble'
import type { ScrobbleValues } from '@/utils/api/types'
import { formatRelative } from '@/utils/format'
import { ws } from '@/utils/ws'

const props = defineProps<{ open: boolean; close: () => void; contentId: string }>()

const FIELDS = [
    { key: 'chapter', label: 'Chapter' },
    { key: 'volume', label: 'Volume' },
    { key: 'state', label: 'Status' },
] as const satisfies { key: keyof ScrobbleValues; label: string }[]

const store = useScrobbleStore()
const qAccount = scrobbleApi.useAccount()
const q = scrobbleApi.useContent(() => props.contentId)
const mHold = scrobbleApi.useHold(() => props.contentId)
const mSend = scrobbleApi.useSendSeries(() => props.contentId)
const data = computed(() => q.data.value)
const now = useNow({ interval: 30_000 })

// The queue row is live over the WebSocket; the dialog's fetch only until the queue has loaded.
const live = computed(() => {
    if (!store.loaded) {
        const d = data.value
        return { state: d?.state, due_at: d?.due_at, error: d?.error }
    }
    const r = store.row(props.contentId)
    return { state: r?.state, due_at: r?.due_at ?? undefined, error: r?.error ?? undefined }
})
const held = computed(() => live.value.state === 'held')

// Any scrobble change, including a linked series' on another device, changes what is sent and what
// MangaBaka holds. Only while mounted: each refetch reads MangaBaka live. A change during a fetch
// can't reuse that fetch, so it refetches once the fetch settles.
const stale = ref(false)
const refetch = () => {
    if (q.isFetching.value) stale.value = true
    else void q.refetch()
}
watch(q.isFetching, fetching => {
    if (fetching || !stale.value) return
    stale.value = false
    void q.refetch()
})
const unsubs = [ws.on('scrobbles_changed', refetch), ws.on('$open', refetch)]
onUnmounted(() => unsubs.forEach(unsub => unsub()))

function show(v: number | string | undefined): string {
    if (v == null || v === '') return '—'
    return typeof v === 'number' ? String(v) : capitalize(v.replaceAll('_', ' '))
}

// The fields MangaBaka keeps over ours, which only Overwrite lowers.
const ahead = computed(() => data.value?.ahead ?? [])

const aheadMessage = computed(() => {
    const names = ahead.value.map(k => FIELDS.find(f => f.key === k)!.label.toLowerCase())
    const list =
        names.length > 1 ? `${names.slice(0, -1).join(', ')} and ${names.at(-1)}` : names[0]
    return `MangaBaka's ${list} ${names.length > 1 ? 'are' : 'is'} ahead and won't be lowered.`
})

const status = computed(() => {
    const { state, due_at } = live.value
    if (state === 'pending' && qAccount.data.value?.rejected) {
        return 'Pending, paused until you reconnect MangaBaka'
    }
    if (state === 'pending' && due_at) {
        return Date.parse(due_at) <= now.value.getTime()
            ? 'Pending, sending now'
            : `Pending, sends ${formatRelative(due_at, now.value.getTime())}`
    }
    if (state === 'pending') return 'Pending'
    const sent = data.value?.sent_at
    return sent ? `Last sent ${formatRelative(sent, now.value.getTime())}` : ''
})

// The POST only queues the send. A settled fetch started after it, with the series no longer
// pending, ends the wait; a rejected token or 30s gives up.
const postedAt = ref<number | null>(null)
const busy = computed(() => mSend.isPending.value || postedAt.value != null)
let waitTimer: ReturnType<typeof setTimeout> | undefined
const stopWaiting = () => {
    postedAt.value = null
    clearTimeout(waitTimer)
}
watchEffect(() => {
    const at = postedAt.value
    if (at == null) return
    const settled = !q.isFetching.value && !stale.value
    const fresh = q.dataUpdatedAt.value >= at && data.value?.state !== 'pending'
    if (qAccount.data.value?.rejected || (settled && fresh)) stopWaiting()
})
onUnmounted(() => clearTimeout(waitTimer))

// A failure shows in the dialog.
const send = (force: boolean) =>
    mSend.mutate(
        { force },
        {
            onSuccess: () => {
                postedAt.value = Date.now()
                waitTimer = setTimeout(stopWaiting, 30_000)
            },
        }
    )
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ScrobbleModal.vue'

export function showScrobbleModal(contentId: string): Promise<void> {
    return Modals.show<void>(Self, { contentId })
}
</script>

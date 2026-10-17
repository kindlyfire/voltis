<template>
    <div
        v-if="store.visible"
        ref="root"
        class="flex flex-col"
        :class="{ 'border-outline-variant mb-1 border-b pb-1': !full }"
    >
        <div class="flex min-h-9 items-center gap-2">
            <AButton
                v-if="!full && store.pending.length"
                variant="text"
                tone="neutral"
                size="sm"
                class="-ml-2 min-w-0"
                :leading-icon="expanded ? IconChevronDown : IconChevronRight"
                :aria-expanded="expanded"
                :aria-controls="listId"
                @click="expanded = !expanded"
            >
                {{ summary }}
            </AButton>
            <span v-else class="min-w-0 text-sm font-semibold">{{ summary }}</span>
            <AButton
                v-if="store.pending.length"
                variant="text"
                size="sm"
                class="-mr-2 ml-auto"
                :loading="working === 'all'"
                @click="send()"
            >
                Send now
            </AButton>
        </div>
        <ul v-if="store.failed.length" class="flex flex-col">
            <li v-for="r in store.failed" :key="r.content_id" class="flex items-center gap-2 py-1">
                <span class="min-w-0 flex-1 text-sm [overflow-wrap:anywhere]">
                    <RouterLink
                        :to="{ name: 'info-page', params: { id: r.content_id } }"
                        class="a-focus rounded-field font-medium hover:underline"
                        @click="emit('navigate')"
                    >
                        {{ r.title }}
                    </RouterLink>
                    <span class="text-error"> · Failed: {{ r.error }}</span>
                </span>
                <AButton
                    variant="text"
                    size="sm"
                    class="-mr-2"
                    :loading="working === r.content_id"
                    :aria-label="`Retry ${r.title}`"
                    @click="send(r.content_id)"
                >
                    Retry
                </AButton>
            </li>
        </ul>
        <ul v-show="full || expanded" :id="listId" class="flex flex-col">
            <li v-for="r in store.pending" :key="r.content_id" class="py-1 text-sm">
                <RouterLink
                    :to="{ name: 'info-page', params: { id: r.content_id } }"
                    class="a-focus rounded-field font-medium [overflow-wrap:anywhere] hover:underline"
                    @click="emit('navigate')"
                >
                    {{ r.title }}
                </RouterLink>
                <span class="text-fg-muted">{{ details(r) }}</span>
            </li>
        </ul>
    </div>
</template>

<script setup lang="ts">
import { useNow } from '@vueuse/core'
import { computed, nextTick, ref, useId, useTemplateRef } from 'vue'
import { RouterLink } from 'vue-router'
import { useScrobbleStore } from '@/stores/scrobbles'
import AButton from '@/ui/AButton.vue'
import { IconChevronDown, IconChevronRight } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { scrobbleApi } from '@/utils/api/scrobble'
import type { ScrobbleRow } from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import { formatRelative } from '@/utils/format'

/** `full` lists the pending series without a disclosure. Hidden while nothing is pending or failed. */
defineProps<{ full?: boolean }>()
const emit = defineEmits<{ navigate: [] }>()

const store = useScrobbleStore()
const qAccount = scrobbleApi.useAccount()
const root = useTemplateRef('root')
const toast = useToast()
const listId = useId()
const expanded = ref(false)
const working = ref<string | null>(null)
const now = useNow({ interval: 30_000 })

const summary = computed(() => {
    const parts = []
    if (store.pending.length) parts.push(`${store.pending.length} pending`)
    if (store.failed.length) parts.push(`${store.failed.length} failed`)
    return `Scrobbling · ${parts.join(', ')}`
})

function details(r: ScrobbleRow): string {
    const values = [
        r.values.chapter != null && `ch. ${r.values.chapter}`,
        r.values.volume != null && `vol. ${r.values.volume}`,
    ].filter(Boolean)
    const parts = values.length ? [values.join(', ')] : []
    if (qAccount.data.value?.rejected) parts.push('paused · reconnect MangaBaka')
    else if (r.due_at) {
        parts.push(
            Date.parse(r.due_at) <= now.value.getTime()
                ? 'sending now'
                : `sends ${formatRelative(r.due_at, now.value.getTime())}`
        )
    }
    return parts.map(p => ` · ${p}`).join('')
}

async function send(id?: string): Promise<void> {
    working.value = id ?? 'all'
    try {
        await store.send(id ? [id] : undefined)
        // A retried row leaves the failed list, and its focused button with it.
        await nextTick()
        if (id && !root.value?.contains(document.activeElement)) {
            root.value?.querySelector<HTMLElement>('button')?.focus()
        }
    } catch (err) {
        toast.show({ message: `Couldn't send: ${RequestError.getMessage(err)}`, tone: 'danger' })
    } finally {
        working.value = null
    }
}
</script>

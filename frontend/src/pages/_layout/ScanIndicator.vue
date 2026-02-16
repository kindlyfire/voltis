<template>
    <VMenu v-if="active" :close-on-content-click="false" max-width="350">
        <template #activator="{ props }">
            <VBtn v-bind="props" icon="mdi-sync" variant="text" class="scan-spin" />
        </template>
        <VCard>
            <VCardText class="space-y-3!">
                <div
                    v-for="item in scanStatus"
                    :key="item.library_id"
                    class="border rounded pa-3 min-w-[300px]"
                >
                    <div class="font-medium flex items-center gap-2">
                        {{ item.library_name }}
                        <VIcon
                            v-if="item.status === 'done'"
                            icon="mdi-check"
                            size="small"
                            color="success"
                        />
                    </div>
                    <template v-if="item.status === 'running'">
                        <VProgressLinear
                            :model-value="
                                !item.progress
                                    ? 0
                                    : item.progress.total === 0
                                      ? 100
                                      : (item.progress.processed / item.progress.total) * 100
                            "
                            class="mt-2"
                            rounded
                            height="6"
                        />
                        <div class="text-xs text-medium-emphasis mt-1">
                            {{ item.progress?.processed ?? 0 }} /
                            {{ item.progress?.total ?? '?' }}
                        </div>
                    </template>
                    <template v-else-if="item.status === 'done'">
                        <VProgressLinear
                            :model-value="100"
                            color="success"
                            class="mt-2"
                            rounded
                            height="6"
                        />
                    </template>
                    <div v-else class="text-sm text-medium-emphasis mt-1">Queued</div>
                </div>
            </VCardText>
        </VCard>
    </VMenu>
</template>

<script setup lang="ts">
import { ref, computed, onUnmounted } from 'vue'
import { ws } from '@/utils/ws'

interface ScanStatusItem {
    library_id: string
    library_name: string
    status: 'running' | 'queued' | 'done'
    progress?: { total: number; processed: number }
}

const scanStatus = ref<ScanStatusItem[]>([])
const active = computed(() => scanStatus.value.some(i => i.status !== 'done'))

let clearTimer: ReturnType<typeof setTimeout> | null = null

const unsubscribe = ws.on('scan_status', (msg: { queue: ScanStatusItem[] }) => {
    if (clearTimer) {
        clearTimeout(clearTimer)
        clearTimer = null
    }

    const queueIds = new Set(msg.queue.map(i => i.library_id))

    for (const item of scanStatus.value) {
        if (item.status !== 'done' && !queueIds.has(item.library_id)) {
            item.status = 'done'
        }
    }

    for (const incoming of msg.queue) {
        const existing = scanStatus.value.find(i => i.library_id === incoming.library_id)
        if (existing) {
            Object.assign(existing, incoming)
        } else {
            scanStatus.value.push(incoming)
        }
    }

    if (msg.queue.length === 0) {
        clearTimer = setTimeout(() => {
            scanStatus.value = []
        }, 3000)
    }
})

onUnmounted(() => {
    unsubscribe()
    if (clearTimer) clearTimeout(clearTimer)
})
</script>

<style scoped>
.scan-spin :deep(.mdi-sync) {
    animation: spin 1.5s linear infinite;
}

@keyframes spin {
    from {
        transform: rotate(0deg);
    }
    to {
        transform: rotate(360deg);
    }
}
</style>

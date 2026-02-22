<template>
    <VMenu v-if="visible" v-model="menuOpen" :close-on-content-click="false" max-width="350">
        <template #activator="{ props }">
            <VBtn v-bind="props" icon="mdi-sync" variant="text" :class="active && 'scan-spin'" />
        </template>
        <VCard>
            <VCardText class="space-y-3!">
                <div
                    v-for="item in scans"
                    :key="item.libraryId"
                    class="border rounded pa-3 min-w-[300px]"
                >
                    <div class="font-medium flex items-center gap-2">
                        {{ getLibraryName(item.libraryId) }}
                        <VIcon
                            v-if="item.status === 'completed'"
                            icon="mdi-check"
                            size="small"
                            color="success"
                        />
                        <VIcon
                            v-else-if="item.status === 'failed'"
                            icon="mdi-alert-circle"
                            size="small"
                            color="error"
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
                    <template v-else-if="item.status === 'completed' || item.status === 'failed'">
                        <VProgressLinear
                            :model-value="100"
                            :color="item.status === 'completed' ? 'success' : 'error'"
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
import { computed, watch, onUnmounted } from 'vue'
import { useScanTracker } from '@/utils/ws'
import { librariesApi } from '@/utils/api/libraries'
import { ref } from 'vue'

const { scans, clear } = useScanTracker()
const libraries = librariesApi.useList()
const menuOpen = ref(false)
const pendingClear = ref(false)

const active = computed(() =>
    scans.value.some(i => i.status === 'running' || i.status === 'queued')
)
const visible = computed(() => scans.value.length > 0)
const allDone = computed(
    () =>
        scans.value.length > 0 &&
        scans.value.every(i => i.status === 'completed' || i.status === 'failed')
)

function getLibraryName(id: string): string {
    return libraries.data?.value?.find(l => l.id === id)?.name ?? id
}

let clearTimer: ReturnType<typeof setTimeout> | null = null

function scheduleClear() {
    if (clearTimer) clearTimeout(clearTimer)
    clearTimer = setTimeout(() => {
        clearTimer = null
        if (menuOpen.value) {
            pendingClear.value = true
        } else {
            clear()
        }
    }, 10000)
}

watch(allDone, done => {
    if (done) scheduleClear()
})

watch(menuOpen, open => {
    if (!open && pendingClear.value) {
        pendingClear.value = false
        clear()
    }
})

onUnmounted(() => {
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

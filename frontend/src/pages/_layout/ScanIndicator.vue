<template>
    <VMenu v-if="visible" v-model="menuOpen" :close-on-content-click="false" max-width="350">
        <template #activator="{ props }">
            <VBtn v-bind="props" icon="mdi-sync" variant="text" :class="active && 'scan-spin'" />
        </template>
        <VCard>
            <VCardText class="flex flex-col gap-3">
                <div v-for="row in rows" :key="row.id" class="min-w-[300px] rounded border p-3">
                    <div class="flex items-center gap-2 font-medium">
                        {{ getLibraryName(row.libraryId) }}
                        <VIcon
                            v-if="row.status >= TaskStatus.COMPLETED"
                            :icon="
                                row.status === TaskStatus.COMPLETED
                                    ? 'mdi-check'
                                    : 'mdi-alert-circle'
                            "
                            :color="row.color"
                            size="small"
                        />
                    </div>
                    <VProgressLinear
                        v-if="row.status !== TaskStatus.PENDING"
                        :model-value="row.value"
                        :indeterminate="row.indeterminate"
                        :color="row.color"
                        class="mt-2"
                        rounded
                        height="6"
                    />
                    <div
                        class="mt-1 opacity-60"
                        :class="row.status === TaskStatus.PENDING ? 'text-sm' : 'text-xs'"
                    >
                        {{ row.detail }}
                    </div>
                </div>
            </VCardText>
        </VCard>
    </VMenu>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { isTerminal, scanRow, useScanStore } from '@/stores/scans'
import { librariesApi } from '@/utils/api/libraries'
import { TaskStatus } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'

const store = useScanStore()
const qMe = usersApi.useMe()
const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))
const libraries = librariesApi.useList()
const menuOpen = ref(false)
const pendingDismiss = new Set<string>()

const shown = computed(() =>
    Object.values(store.tasks).filter(task => !store.dismissed.has(task.id))
)
const rows = computed(() => shown.value.map(scanRow))
const active = computed(() => shown.value.some(task => !isTerminal(task)))
const visible = computed(() => isAdmin.value && rows.value.length > 0)

function getLibraryName(id: string): string {
    return libraries.data?.value?.find(l => l.id === id)?.name ?? id
}

const DISMISS_DELAY = 10000
const dueAt = new Map<string, number>()
let timer: ReturnType<typeof setTimeout> | null = null

function schedule() {
    if (timer) clearTimeout(timer)
    timer = null
    const next = Math.min(...dueAt.values())
    if (!Number.isFinite(next)) return
    timer = setTimeout(sweep, Math.max(0, next - Date.now()))
}

function sweep() {
    timer = null
    const ready = [...dueAt].filter(([, at]) => at - Date.now() <= 1).map(([id]) => id)
    for (const id of ready) dueAt.delete(id)
    if (menuOpen.value) {
        for (const id of ready) pendingDismiss.add(id)
    } else {
        store.dismiss(ready)
    }
    schedule()
}

watch(
    shown,
    tasks => {
        const ids = new Set(tasks.map(task => task.id))
        const terminal = new Set(tasks.filter(isTerminal).map(task => task.id))
        for (const id of terminal) {
            if (!dueAt.has(id) && !pendingDismiss.has(id)) dueAt.set(id, Date.now() + DISMISS_DELAY)
        }
        for (const id of [...dueAt.keys()]) if (!terminal.has(id)) dueAt.delete(id)
        for (const id of [...pendingDismiss]) if (!ids.has(id)) pendingDismiss.delete(id)
        schedule()
    },
    { immediate: true }
)

watch(menuOpen, open => {
    if (open || pendingDismiss.size === 0) return
    store.dismiss([...pendingDismiss])
    pendingDismiss.clear()
})

onUnmounted(() => {
    if (timer) clearTimeout(timer)
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

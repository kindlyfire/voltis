<template>
    <APopover v-if="visible" v-model:open="menuOpen" label="Library scans" align="end" :width="340">
        <template #trigger>
            <AIconButton
                :icon="IconSync"
                :label="active ? 'Library scans (running)' : 'Library scans'"
                :class="{ 'scan-spin': active }"
            />
        </template>
        <ul class="flex flex-col gap-3">
            <li
                v-for="row in rows"
                :key="row.id"
                class="border-outline-variant flex flex-col gap-2 rounded-xl border p-3"
            >
                <div class="flex items-center gap-2 text-sm font-semibold">
                    <span class="min-w-0 truncate">{{ getLibraryName(row.libraryId) }}</span>
                    <AIcon
                        v-if="row.status >= TaskStatus.COMPLETED"
                        :icon="row.status === TaskStatus.COMPLETED ? IconCheck : IconAlertCircle"
                        :label="row.status === TaskStatus.COMPLETED ? 'Done' : 'Failed'"
                        :class="row.tone === 'success' ? 'text-success' : 'text-error'"
                        class="text-lg"
                    />
                </div>
                <AProgressBar
                    v-if="row.status !== TaskStatus.PENDING"
                    :value="row.value / 100"
                    :indeterminate="row.indeterminate"
                    :tone="row.tone"
                    :label="`Scan of ${getLibraryName(row.libraryId)}`"
                    :thickness="6"
                />
                <div class="text-fg-muted text-xs">{{ row.detail }}</div>
            </li>
        </ul>
    </APopover>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { isTerminal, scanRow, useScanStore } from '@/stores/scans'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APopover from '@/ui/APopover.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import { IconAlertCircle, IconCheck, IconSync } from '@/ui/icons'
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
.scan-spin :deep(svg) {
    animation: spin 1.5s linear infinite;
}

@media (prefers-reduced-motion: reduce) {
    .scan-spin :deep(svg) {
        animation: none;
    }
}

@keyframes spin {
    to {
        transform: rotate(360deg);
    }
}
</style>

<template>
    <li class="rounded-menu-item hover:bg-surface-2 flex gap-3 px-2.5 py-2">
        <span
            aria-hidden="true"
            class="relative grid size-8 shrink-0 place-items-center rounded-full"
            :class="TINTS[look.tone]"
        >
            <AIcon
                :icon="look.icon"
                :size="18"
                :class="{ spin: lead && lead.state !== 'queued' }"
            />
            <span
                v-if="item.unread"
                class="bg-primary ring-raised absolute -top-0.5 -right-0.5 size-2.5 rounded-full ring-2"
            />
        </span>
        <div class="flex min-w-0 flex-1 flex-col gap-1">
            <component
                :is="to ? RouterLink : 'button'"
                v-bind="to ? { to } : { type: 'button' }"
                :disabled="!to && !canOpen ? true : undefined"
                class="a-focus rounded-field flex min-w-0 flex-col gap-1 text-left"
                @click="open"
            >
                <span class="flex items-center gap-2">
                    <span
                        class="min-w-0 flex-1 truncate text-sm"
                        :class="item.unread && 'font-semibold'"
                    >
                        <span v-if="item.unread" class="sr-only">Unread. </span>{{ title }}
                    </span>
                    <AChip v-if="look.chip" size="sm" :tone="look.chipTone">{{ look.chip }}</AChip>
                    <span v-else class="text-fg-muted shrink-0 text-xs tabular-nums">{{
                        since
                    }}</span>
                </span>
                <AProgressBar
                    v-if="lead && lead.state !== 'queued'"
                    :value="barValue"
                    :indeterminate="lead.state !== 'parsing'"
                    :label="title"
                    :thickness="3"
                />
                <span v-if="subtitle || warnings.length" class="text-fg-muted text-xs">
                    {{ subtitle }}<template v-if="subtitle && warnings.length"> · </template>
                    <span v-if="warnings.length" class="text-warning">{{
                        warnings.join(' · ')
                    }}</span>
                </span>
            </component>
            <div v-if="canCancel || canRetry || full" class="-my-1.5 -ml-3.5 flex items-center">
                <AButton
                    v-if="canCancel"
                    variant="text"
                    size="sm"
                    :disabled="working"
                    @click="cancel"
                >
                    Cancel
                </AButton>
                <AButton
                    v-if="canRetry"
                    variant="text"
                    size="sm"
                    :disabled="working"
                    @click="retry"
                >
                    Retry
                </AButton>
                <AButton
                    v-if="full && scan && item.task_id && !isActive"
                    variant="text"
                    size="sm"
                    @click="showTaskLogs(item.task_id)"
                >
                    Logs
                </AButton>
                <AButton
                    v-if="full"
                    variant="text"
                    size="sm"
                    class="ml-auto"
                    @click="store.dismiss(item.id)"
                >
                    Dismiss
                </AButton>
            </div>
        </div>
    </li>
</template>

<script setup lang="ts">
import { type Component, computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { showScanModal } from '@/pages/settings/ScanModal.vue'
import { showTaskLogs } from '@/pages/settings/TaskLogsModal.vue'
import { useNotificationStore } from '@/stores/notifications'
import { type ScanLead, scanRow, useScanStore } from '@/stores/scans'
import AButton from '@/ui/AButton.vue'
import AChip from '@/ui/AChip.vue'
import AIcon from '@/ui/AIcon.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import {
    IconAlert,
    IconAlertCircle,
    IconAutoStories,
    IconCheck,
    IconClose,
    IconMagnifyScan,
    IconSync,
} from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { tasksApi } from '@/utils/api/tasks'
import type { Notification } from '@/utils/api/types'
import { RequestError } from '@/utils/fetch'
import { formatRelative } from '@/utils/format'

const props = defineProps<{
    item: Notification
    /** The modal's row: with the dismiss, cancel and logs actions. */
    full?: boolean
}>()
/** The row led away from the center that shows it. */
const emit = defineEmits<{ navigate: [] }>()

const store = useNotificationStore()
const scans = useScanStore()
const toast = useToast()
const working = ref(false)

const scan = computed(() => (props.item.kind === 'scan' ? props.item : null))
const isActive = computed(() => !!scan.value && !scan.value.data.outcome)
const taskId = computed(() => props.item.task_id)
const canOpen = computed(() => !!scan.value && !!taskId.value)

const to = computed(() => {
    const i = props.item
    if (i.kind === 'new_content' && i.content)
        return { name: 'info-page', params: { id: i.content.id } }
    if (i.kind === 'metadata_review') return { name: 'settings-metadata', query: { tab: 'review' } }
    if (i.kind === 'scrobble_auth') return { name: 'settings-scrobbling' }
    return null
})

const title = computed(() => {
    const i = props.item
    switch (i.kind) {
        case 'scan':
            return `Scan of ${i.data.library_name}`
        case 'new_content': {
            const unit = i.data.count === 1 ? i.data.unit : `${i.data.unit}s`
            return `${i.content?.title ?? 'A series'}: ${i.data.count} new ${unit}`
        }
        case 'metadata_review':
            return `${i.data.count} series need review`
        case 'scrobble_auth':
            return 'MangaBaka rejected your token'
    }
})

const lead = computed<ScanLead | null>(() => {
    if (!isActive.value) return null
    const task = taskId.value ? scans.tasks[taskId.value] : undefined
    return task ? scanRow(task).lead : { state: 'queued' }
})

const barValue = computed(() => {
    const l = lead.value
    return l?.state === 'parsing' && l.total > 0 ? l.processed / l.total : 1
})

const since = computed(() => formatRelative(props.item.updated_at))

const subtitle = computed(() => {
    const i = props.item
    if (i.kind === 'metadata_review') return 'Metadata'
    if (i.kind === 'scrobble_auth') return 'Reconnect to resume scrobbling'
    if (i.kind === 'new_content') return ''
    const l = lead.value
    if (l) return progressText(l)
    const { outcome, saved } = i.data
    if (outcome === 'completed') {
        return `${saved?.added ?? 0} added, ${saved?.updated ?? 0} updated, ${saved?.removed ?? 0} removed`
    }
    return since.value
})

type Tone = 'running' | 'input' | 'new' | 'warn' | 'fail' | 'ok' | 'neutral'
const TINTS: Record<Tone, string> = {
    running: 'bg-primary-container text-on-primary-container',
    input: 'bg-warning-container text-on-warning-container',
    new: 'bg-secondary-container text-on-secondary-container',
    warn: 'bg-warning-container text-on-warning-container',
    fail: 'bg-error-container text-on-error-container',
    ok: 'bg-success-container text-on-success-container',
    neutral: 'bg-surface-3 text-fg-muted',
}

// The round icon and, for states worth a label, the chip that replaces the time.
const look = computed<{
    icon: Component
    tone: Tone
    chip?: string
    chipTone?: 'primary' | 'warning' | 'danger' | 'neutral'
}>(() => {
    const i = props.item
    if (i.kind === 'new_content') return { icon: IconAutoStories, tone: 'new' }
    if (i.kind === 'metadata_review')
        return { icon: IconMagnifyScan, tone: 'input', chip: 'Review', chipTone: 'warning' }
    if (i.kind === 'scrobble_auth')
        return { icon: IconAlertCircle, tone: 'input', chip: 'Reconnect', chipTone: 'warning' }
    if (lead.value) {
        const queued = lead.value.state === 'queued'
        return {
            icon: IconSync,
            tone: 'running',
            chip: queued ? 'Queued' : 'Scanning',
            chipTone: 'primary',
        }
    }
    switch (i.data.outcome) {
        case 'failed':
            return { icon: IconAlertCircle, tone: 'fail', chip: 'Failed', chipTone: 'danger' }
        case 'cancelled':
            return { icon: IconClose, tone: 'neutral', chip: 'Cancelled', chipTone: 'neutral' }
        default:
            return warnings.value.length
                ? { icon: IconAlert, tone: 'warn' }
                : { icon: IconCheck, tone: 'ok' }
    }
})

function progressText(l: ScanLead): string {
    switch (l.state) {
        case 'queued':
            return 'Queued'
        case 'walking':
            return `Looking for files, ${l.found} found`
        case 'parsing':
            return `${l.processed} / ${l.total}`
        case 'saving':
            return 'Saving'
        case 'done':
            return 'Done'
    }
}

const warnings = computed(() => {
    const d = scan.value?.data
    if (!d?.outcome) return []
    const out: string[] = []
    if (d.failed) out.push(`${d.failed} ${d.failed === 1 ? 'file' : 'files'} failed`)
    if (d.read_failures) out.push("Some folders couldn't be read")
    if (d.removals_suppressed) out.push('Nothing removed')
    return out
})

const canCancel = computed(() => isActive.value && !!taskId.value)
const canRetry = computed(() => {
    const d = scan.value?.data
    return (
        !!d?.outcome &&
        !!taskId.value &&
        !d.library_deleted &&
        (d.outcome !== 'completed' || warnings.value.length > 0)
    )
})

function open(): void {
    if (to.value) return emit('navigate')
    if (!taskId.value) return
    if (!props.full) emit('navigate')
    void showScanModal(
        { taskIds: [taskId.value] },
        { focusFallback: () => document.querySelector<HTMLElement>('[data-notifications-trigger]') }
    )
}

async function run(action: () => Promise<unknown>, failure: string): Promise<void> {
    working.value = true
    try {
        await action()
    } catch (err) {
        // A scan that already ended can't be cancelled; nothing to say.
        if (!(err instanceof RequestError && err.response?.status === 409)) {
            toast.show({ message: `${failure}: ${RequestError.getMessage(err)}`, tone: 'danger' })
        }
    } finally {
        working.value = false
    }
}

const cancel = () => run(() => tasksApi.cancel(taskId.value!), "Couldn't cancel")
const retry = () =>
    run(async () => {
        await tasksApi.retry(taskId.value!)
        await store.refresh()
    }, "Couldn't retry")
</script>

<style scoped>
.spin {
    animation: spin 1.5s linear infinite;
}
@media (prefers-reduced-motion: reduce) {
    .spin {
        animation: none;
    }
}
@keyframes spin {
    to {
        transform: rotate(360deg);
    }
}
</style>

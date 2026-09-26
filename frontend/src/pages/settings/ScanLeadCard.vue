<template>
    <div class="flex flex-col gap-1.5">
        <div
            class="scan-lead bg-surface-2 text-fg-muted flex flex-col items-center justify-center gap-2 p-3 text-center text-xs"
        >
            <template v-if="lead.state === 'walking'">
                <ASpinner decorative />
                <span>{{ lead.found }} found</span>
            </template>
            <template v-else-if="lead.state === 'parsing'">
                <AProgressBar
                    class="w-full"
                    :value="lead.total > 0 ? lead.processed / lead.total : 1"
                    :label="label"
                    :value-text="`${lead.processed} of ${lead.total} files`"
                    :thickness="6"
                />
                <span>{{ lead.total - lead.processed }} files left</span>
            </template>
            <ASpinner v-else-if="lead.state === 'saving'" decorative />
            <template v-else-if="lead.state === 'done'">
                <AIcon
                    :icon="lead.outcome === 'completed' ? IconCheck : IconAlertCircle"
                    :class="lead.outcome === 'completed' ? 'text-success' : 'text-error'"
                    class="text-5xl"
                />
                <ScanCounts :counts="lead.counts" all class="justify-center" />
            </template>
        </div>
        <p class="text-xs leading-snug font-medium">{{ caption }}</p>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ScanLead } from '@/stores/scans'
import AIcon from '@/ui/AIcon.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconAlertCircle, IconCheck } from '@/ui/icons'
import ScanCounts from './ScanCounts.vue'

const props = defineProps<{
    lead: ScanLead
    /** Accessible name of the progress bar. */
    label: string
}>()

type Done = Extract<ScanLead, { state: 'done' }>

const captions: Record<Exclude<ScanLead['state'], 'done'>, string> = {
    queued: 'Queued',
    walking: 'Looking for files',
    parsing: 'Reading files',
    saving: 'Saving',
}
const outcomes: Record<Done['outcome'], string> = {
    completed: 'Done',
    failed: 'Failed',
    cancelled: 'Cancelled',
}

const caption = computed(() => {
    const lead = props.lead
    return lead.state === 'done' ? outcomes[lead.outcome] : captions[lead.state]
})
</script>

<style scoped>
.scan-lead {
    aspect-ratio: 2 / 3;
    border-radius: var(--radius-cover);
}
</style>

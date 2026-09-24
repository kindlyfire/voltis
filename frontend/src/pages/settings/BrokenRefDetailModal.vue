<template>
    <ADialog :open="open" title="Reference detail" size="lg" @update:open="v => !v && close()">
        <pre
            class="bg-surface-2 rounded-field max-h-[60dvh] overflow-auto p-3 text-xs leading-snug wrap-break-word whitespace-pre-wrap"
            tabindex="0"
            aria-label="Reference data"
            >{{ formatted }}</pre>
        <template #actions>
            <AButton variant="text" tone="neutral" autofocus @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import type { BrokenUserToContent } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    item: BrokenUserToContent
}>()

const formatted = computed(() => JSON.stringify(props.item, null, 2))
</script>

<script lang="ts">
import type { BrokenUserToContent as BUC } from '@/utils/api/types'
import { Modals } from '@/utils/modals'
import Self from './BrokenRefDetailModal.vue'

export function showBrokenRefDetailModal(item: BUC): Promise<void> {
    return Modals.show<void>(Self, { item })
}
</script>

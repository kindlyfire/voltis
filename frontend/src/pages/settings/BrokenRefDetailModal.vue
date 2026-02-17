<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="600">
        <VCard>
            <VCardTitle>Reference Detail</VCardTitle>
            <VCardText>
                <code>
                    <pre class="overflow-auto bg-surface-variant rounded pa-2 font-mono!">{{
                        formatted
                    }}</pre>
                </code>
            </VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()">Close</VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { BrokenUserToContent } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    item: BrokenUserToContent
}>()

const formatted = computed(() => JSON.stringify(props.item, null, 2))
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import type { BrokenUserToContent as BUC } from '@/utils/api/types'
import Self from './BrokenRefDetailModal.vue'

export function showBrokenRefDetailModal(item: BUC): Promise<void> {
    return Modals.show<void>(Self, { item })
}
</script>

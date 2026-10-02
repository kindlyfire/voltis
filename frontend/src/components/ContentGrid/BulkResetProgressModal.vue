<template>
    <ADialog
        :open="open"
        title="Clear status and position"
        :description="`This clears the status, position and reading time of ${plural(contentIds.length, 'item')}, and of the volumes of any series among them${contentTitles ? ':' : '.'}`"
        :dismissible="!mBulk.isPending.value"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <ul
                v-if="contentTitles"
                class="bg-surface-2 rounded-field max-h-60 overflow-y-auto px-4 py-2"
                tabindex="0"
                aria-label="Selected items"
            >
                <li
                    v-for="(title, i) in contentTitles"
                    :key="contentIds[i]"
                    class="border-outline-variant truncate border-t py-2 first:border-t-0"
                >
                    {{ title }}
                </li>
            </ul>
            <QueryError :mutation="mBulk" />
        </div>
        <template #actions>
            <AButton
                variant="text"
                tone="neutral"
                :disabled="mBulk.isPending.value"
                @click="close()"
            >
                Cancel
            </AButton>
            <AButton tone="danger" :loading="mBulk.isPending.value" @click="mBulk.mutate()">
                Clear
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation } from '@tanstack/vue-query'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { plural } from '@/utils/misc'

const props = defineProps<{
    open: boolean
    close: (done?: boolean) => void
    contentIds: string[]
    /** In `contentIds` order; without them the dialog shows only the count. */
    contentTitles?: string[]
}>()

const toast = useToast()

const mBulk = useMutation({
    mutationFn: () => contentApi.bulkUserData({ ids: props.contentIds, action: 'reset' }),
    onSuccess: ({ count }) => {
        toast.show({ message: `Cleared the status and position of ${plural(count, 'item')}` })
        props.close(true)
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './BulkResetProgressModal.vue'

/** Resolves true once the progress was reset. */
export function showBulkResetProgressModal(
    contentIds: string[],
    contentTitles?: string[]
): Promise<boolean> {
    return Modals.show<boolean | undefined>(Self, { contentIds, contentTitles }).then(
        done => done === true
    )
}
</script>

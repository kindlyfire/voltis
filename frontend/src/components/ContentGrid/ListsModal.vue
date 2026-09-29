<template>
    <ADialog
        :open="open"
        title="Add to lists"
        :description="`${plural(contentIds.length, 'item')} selected`"
        size="sm"
        :dismissible="!mBulk.isPending.value"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <QueryError :query="qLists" />

            <div v-if="qLists.isLoading.value" class="flex justify-center py-8">
                <ASpinner />
            </div>

            <template v-else-if="qLists.isSuccess.value">
                <fieldset class="flex flex-col">
                    <legend class="sr-only">Lists</legend>
                    <ACheckbox
                        v-for="list in qLists.data.value ?? []"
                        :key="list.id"
                        :model-value="selectedListIds.has(list.id)"
                        :label="list.name"
                        @update:model-value="toggleList(list.id)"
                    >
                        {{ list.name }}
                        <span class="text-fg-muted text-sm capitalize"
                            >· {{ list.visibility }}</span
                        >
                    </ACheckbox>
                </fieldset>
                <NewListForm
                    v-if="newList.creating.value"
                    @created="preselect"
                    @close="newList.stop"
                />
            </template>

            <QueryError :mutation="mBulk" />
        </div>
        <template #actions>
            <AButton
                v-if="qLists.isSuccess.value && !newList.creating.value"
                :ref="newList.buttonRef"
                class="mr-auto"
                variant="text"
                :leading-icon="IconPlus"
                @click="newList.start"
            >
                New list
            </AButton>
            <!-- Keeps Cancel and Add together when the footer wraps. -->
            <div class="flex gap-2">
                <AButton
                    variant="text"
                    tone="neutral"
                    :disabled="mBulk.isPending.value"
                    @click="close()"
                >
                    Cancel
                </AButton>
                <AButton
                    :loading="mBulk.isPending.value"
                    :disabled="selectedListIds.size === 0"
                    @click="save"
                >
                    Add
                </AButton>
            </div>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import NewListForm, { useNewListToggle } from '@/components/NewListForm.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconPlus } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { customListsApi } from '@/utils/api/custom-lists'
import type { CustomListPartial } from '@/utils/api/types'
import { plural } from '@/utils/misc'

const props = defineProps<{
    open: boolean
    close: (added?: boolean) => void
    contentIds: string[]
}>()

const newList = useNewListToggle()

const qLists = customListsApi.useList('me')
const mBulk = customListsApi.useBulkCreateEntries()
const toast = useToast()

const selectedListIds = ref(new Set<string>())

function toggleList(id: string) {
    const next = new Set(selectedListIds.value)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selectedListIds.value = next
}

// Checked once `qLists` refetches; "Add" then adds the items to it.
function preselect(list: CustomListPartial) {
    selectedListIds.value = new Set(selectedListIds.value).add(list.id)
}

function save() {
    const lists = plural(selectedListIds.value.size, 'list')
    mBulk.mutate(
        { list_ids: [...selectedListIds.value], ids: props.contentIds },
        {
            onSuccess: ({ count }) => {
                toast.show({ message: `Added ${plural(count, 'entry', 'entries')} to ${lists}` })
                props.close(true)
            },
        }
    )
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ListsModal.vue'

/** Resolves true once the entries were added. */
export function showBulkListsModal(contentIds: string[]): Promise<boolean> {
    return Modals.show<boolean | undefined>(Self, { contentIds }).then(added => added === true)
}
</script>

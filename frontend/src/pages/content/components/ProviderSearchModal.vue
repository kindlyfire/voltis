<template>
    <ADialog
        :open="open"
        :title="`Search ${link.label}`"
        size="lg"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <ATextField
                v-model="searchInput"
                :label="`Title, ${link.label} ID or URL`"
                type="search"
                :leading-icon="IconMagnify"
                :loading="qCandidates.isFetching.value"
                autofocus
                @keydown.enter="!$event.isComposing && $event.keyCode !== 229 && commitSearch()"
            />

            <p
                v-if="showingStored"
                class="text-fg-muted flex flex-wrap items-center gap-x-2 text-sm"
            >
                The candidates of the last match.
                <AButton
                    size="sm"
                    variant="text"
                    :leading-icon="IconMagnify"
                    @click="commitSearch()"
                >
                    Search {{ link.label }}
                </AButton>
            </p>

            <QueryError :query="qCandidates" />

            <div
                v-if="qCandidates.isFetching.value && !candidates.length"
                class="flex justify-center py-10"
            >
                <ASpinner label="Searching" />
            </div>

            <ul v-else-if="candidates.length" class="flex flex-col gap-2" aria-label="Results">
                <li v-for="item in candidates" :key="item.key.id">
                    <ACard variant="tonal" padding="md" class="flex-row flex-wrap gap-3">
                        <ACover :src="item.cover_url" alt="" class="w-16 shrink-0 self-start" />
                        <div class="flex min-w-0 flex-1 flex-col gap-1.5">
                            <h3 :id="`${idPrefix}-${item.key.id}`" class="text-base font-medium">
                                {{ item.title }}
                            </h3>
                            <div class="flex flex-wrap gap-1.5">
                                <AChip v-if="item.key.id === currentId" size="sm" tone="primary">
                                    Current
                                </AChip>
                                <AChip v-if="item.kind" size="sm">{{ item.kind }}</AChip>
                                <AChip v-if="item.status" size="sm">{{ item.status }}</AChip>
                                <AChip v-if="item.year" size="sm">{{ item.year }}</AChip>
                            </div>
                            <p v-if="item.staff.length" class="text-fg-muted text-xs">
                                {{ item.staff.join(', ') }}
                            </p>
                            <div class="flex flex-wrap gap-1.5" aria-label="How it matches">
                                <AChip
                                    v-for="chip in evaluationChips(item.evaluation)"
                                    :key="chip.label"
                                    size="sm"
                                    variant="outlined"
                                    :tone="chip.tone"
                                >
                                    {{ chip.label }}
                                </AChip>
                            </div>
                        </div>
                        <div
                            class="flex shrink-0 items-center gap-2 max-sm:w-full max-sm:justify-end"
                        >
                            <AButton
                                size="sm"
                                variant="text"
                                :href="item.url"
                                :aria-describedby="`${idPrefix}-${item.key.id}`"
                            >
                                Open
                            </AButton>
                            <AButton
                                size="sm"
                                :loading="isPending(item.key.id)"
                                :disabled="mAction.isPending.value && !isPending(item.key.id)"
                                :aria-describedby="`${idPrefix}-${item.key.id}`"
                                @click="select(item)"
                            >
                                Select
                            </AButton>
                        </div>
                    </ACard>
                </li>
            </ul>

            <p v-else-if="qCandidates.isSuccess.value" class="text-fg-muted py-4 text-center">
                No results found.
            </p>

            <QueryError :mutation="mAction" />
        </div>
        <template #actions>
            <AButton
                v-if="link.state !== 'ignored'"
                variant="text"
                tone="danger"
                class="mr-auto"
                :loading="mAction.isPending.value && mAction.variables.value?.action === 'reject'"
                :disabled="mAction.isPending.value || !canReject"
                @click="reject()"
            >
                None of these
            </AButton>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AChip from '@/ui/AChip.vue'
import ACover from '@/ui/ACover.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconMagnify } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import {
    metadataApi,
    type Candidate,
    type LinkAction,
    type MetadataLink,
} from '@/utils/api/metadata'
import { evaluationChips } from './evaluationChips'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
    link: MetadataLink
    title: string
}>()

const toast = useToast()
const idPrefix = useId()
const currentId = computed(() => props.link.entry?.key.id ?? props.link.external_id)

// Stored candidates show first; searching the provider again is up to the admin.
const searchInput = ref(props.title)
const committedQuery = ref<string | null>(props.link.candidates.length ? null : props.title)
const showingStored = computed(() => committedQuery.value === null)

let debounceTimer: ReturnType<typeof setTimeout> | undefined
onBeforeUnmount(() => clearTimeout(debounceTimer))

function commitSearch() {
    clearTimeout(debounceTimer)
    committedQuery.value = searchInput.value.trim()
}

watch(searchInput, () => {
    clearTimeout(debounceTimer)
    debounceTimer = setTimeout(commitSearch, 400)
})

const qCandidates = metadataApi.useCandidates(
    () => props.contentId,
    () => props.link.provider,
    committedQuery
)
const candidates = computed(() =>
    showingStored.value ? props.link.candidates : (qCandidates.data.value?.data ?? [])
)

const mAction = metadataApi.useLinkAction()

function isPending(id: string) {
    const v = mAction.variables.value
    return mAction.isPending.value && v?.action === 'link' && v.externalId === id
}

function run(action: LinkAction, message: string) {
    mAction.mutate(action, {
        onSuccess() {
            toast.show({ message })
            props.close()
        },
    })
}

function select(item: Candidate) {
    const { contentId, link } = props
    run(
        {
            action: 'link',
            contentId,
            provider: link.provider,
            externalId: item.key.id,
            rev: link.rev,
        },
        `Linked to ${item.title} on ${link.label}`
    )
}

// Rejecting nothing would only drop the stored candidates.
const canReject = computed(
    () =>
        !qCandidates.isFetching.value &&
        (candidates.value.length > 0 || props.link.state === 'linked')
)

/** Rejects the candidates shown, and a linked entry; matching goes on without them. */
function reject() {
    const { contentId, link } = props
    run(
        {
            action: 'reject',
            contentId,
            provider: link.provider,
            externalIds: candidates.value.map(c => c.key.id),
            rev: link.rev,
        },
        `Rejected the ${link.label} candidates`
    )
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ProviderSearchModal.vue'

export function showProviderSearchModal(
    contentId: string,
    link: MetadataLink,
    title: string
): Promise<void> {
    return Modals.show(Self, { contentId, link, title })
}
</script>

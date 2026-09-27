<template>
    <ACard variant="tonal" padding="md" :title="link.label" :heading-level="3">
        <template v-if="link.state === 'linked'">
            <p>
                <a
                    v-if="link.entry"
                    :href="link.entry.url"
                    target="_blank"
                    rel="noopener"
                    class="a-focus text-primary rounded-sm font-medium hover:underline"
                >
                    {{ link.entry.title }}
                    <span class="sr-only">(opens in a new tab)</span>
                </a>
                <template v-else>#{{ link.external_id }}</template>
                <span v-if="link.entry?.year" class="text-fg-muted"> ({{ link.entry.year }})</span>
            </p>
            <p class="text-fg-muted text-xs">
                {{ link.origin === 'auto' ? 'Matched automatically' : 'Linked manually' }}
                <template v-if="link.fetched_at"> · Fetched {{ date(link.fetched_at) }}</template>
                <template v-if="link.refresh_at && !link.refresh_error">
                    · Next refresh {{ date(link.refresh_at) }}
                </template>
            </p>
            <AAlert v-if="link.deleted" tone="warning">
                Deleted on {{ link.label }}; its last data is kept.
            </AAlert>
            <AAlert v-if="link.last_error" tone="danger">
                The stored {{ link.label }} data can't be read, so it's left out:
                {{ link.last_error }}
            </AAlert>
            <AAlert v-if="link.refresh_error" tone="warning">
                Refreshing failed {{ plural(link.refresh_attempts, 'time') }}:
                {{ link.refresh_error }}. The last data is kept; next try
                {{ date(link.refresh_at!) }}.
            </AAlert>
        </template>
        <template v-else-if="top">
            <p>
                <a
                    :href="top.url"
                    target="_blank"
                    rel="noopener"
                    class="a-focus text-primary rounded-sm font-medium hover:underline"
                >
                    {{ top.title }}
                    <span class="sr-only">(opens in a new tab)</span>
                </a>
                <span v-if="top.year" class="text-fg-muted"> ({{ top.year }})</span>
            </p>
            <div class="flex flex-wrap gap-1.5" aria-label="How it matches">
                <AChip
                    v-for="chip in evaluationChips(top.evaluation)"
                    :key="chip.label"
                    size="sm"
                    variant="outlined"
                    :tone="chip.tone"
                >
                    {{ chip.label }}
                </AChip>
            </div>
            <p class="text-fg-muted text-xs">
                The best of {{ plural(link.candidates.length, 'candidate') }} waiting for review
            </p>
        </template>
        <p v-else-if="link.last_error" class="text-danger">
            Matching failed: {{ link.last_error }}
        </p>
        <p v-else class="text-fg-muted">{{ STATE_TEXT[link.state] }}</p>

        <QueryError :mutation="mAction" />

        <div class="flex flex-wrap gap-2">
            <AButton
                v-if="top"
                size="sm"
                :loading="isPending('link')"
                :disabled="mAction.isPending.value"
                @click="
                    run(
                        {
                            action: 'link',
                            ...target,
                            externalId: top.key.id,
                            rev: link.rev,
                        },
                        `Linked to ${top.title} on ${link.label}`
                    )
                "
            >
                Accept
            </AButton>
            <AButton size="sm" variant="tonal" :leading-icon="IconMagnify" @click="search()">
                {{ SEARCH_LABEL[link.state] }}
            </AButton>
            <AButton
                v-if="link.state !== 'linked'"
                size="sm"
                variant="tonal"
                :leading-icon="IconSync"
                :loading="isPending('rematch')"
                :disabled="mAction.isPending.value"
                @click="
                    run(
                        { action: 'rematch', ...target, rev: link.rev },
                        `Matched on ${link.label} again`
                    )
                "
            >
                Rematch
            </AButton>
            <AButton
                v-if="link.state === 'linked'"
                size="sm"
                variant="tonal"
                :leading-icon="IconRefresh"
                :loading="isPending('refresh')"
                :disabled="mAction.isPending.value"
                @click="run({ action: 'refresh', ...target }, `Refreshed from ${link.label}`)"
            >
                Refresh
            </AButton>
            <AButton
                v-if="link.state === 'linked' || link.state === 'review'"
                size="sm"
                variant="text"
                tone="danger"
                :leading-icon="IconLinkOff"
                :loading="isPending('ignore')"
                :disabled="mAction.isPending.value"
                @click="
                    run(
                        { action: 'ignore', ...target, rev: link.rev },
                        `Ignored ${link.label} for this series`
                    )
                "
            >
                Ignore
            </AButton>
        </div>
    </ACard>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AChip from '@/ui/AChip.vue'
import { IconLinkOff, IconMagnify, IconRefresh, IconSync } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { metadataApi, type LinkAction, type MetadataLink } from '@/utils/api/metadata'
import { plural } from '@/utils/misc'
import { evaluationChips } from './evaluationChips'
import { showProviderSearchModal } from './ProviderSearchModal.vue'

const props = defineProps<{
    contentId: string
    link: MetadataLink
    /** Prefills the search. */
    title: string
}>()

const STATE_TEXT: Record<MetadataLink['state'], string> = {
    none: 'Not linked.',
    review: 'Candidates are waiting for review.',
    unmatched: 'No match found.',
    linked: '',
    ignored: 'Ignored: this series is not on it.',
}

const SEARCH_LABEL: Record<MetadataLink['state'], string> = {
    none: 'Search…',
    review: 'Other candidates…',
    unmatched: 'Search…',
    linked: 'Change…',
    ignored: 'Search…',
}

const top = computed(() => (props.link.state === 'review' ? props.link.candidates[0] : undefined))

const date = (iso: string) => new Date(iso).toLocaleString()

const toast = useToast()
const target = computed(() => ({
    contentId: props.contentId,
    provider: props.link.provider,
}))
const mAction = metadataApi.useLinkAction()

function isPending(action: LinkAction['action']) {
    return mAction.isPending.value && mAction.variables.value?.action === action
}

function run(action: LinkAction, message: string) {
    mAction.mutate(action, { onSuccess: () => toast.show({ message }) })
}

function search() {
    showProviderSearchModal(props.contentId, props.link, props.title)
}
</script>

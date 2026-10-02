<template>
    <section class="relative isolate" aria-labelledby="content-title">
        <!-- Decorative: the cover, blurred, fading into the page behind the header. -->
        <div v-if="coverUri" class="info-backdrop" aria-hidden="true">
            <div :style="{ backgroundImage: `url(${coverUri})` }" />
        </div>

        <div class="grid gap-6 sm:grid-cols-[180px_1fr] sm:gap-8 lg:grid-cols-[240px_1fr]">
            <div
                class="flex w-44 flex-col gap-3 justify-self-center sm:w-auto sm:justify-self-stretch"
            >
                <button
                    v-if="coverUri"
                    type="button"
                    class="cover-button"
                    :aria-label="`View the cover of ${content.title}`"
                    @click="showCover = true"
                >
                    <ACover :src="coverUri" alt="" class="shadow-lg" />
                </button>
                <ACover v-else :src="null" alt="" />
                <CoverProgress :content="content" />
            </div>

            <div class="flex min-w-0 flex-col gap-5">
                <header
                    class="flex flex-col items-center gap-2 text-center sm:items-start sm:text-left"
                >
                    <RouterLink
                        v-if="parent"
                        :to="`/${parent.id}`"
                        class="a-focus text-primary flex items-center rounded-sm text-sm font-semibold hover:underline sm:mt-2"
                    >
                        <AIcon :icon="IconChevronLeft" class="-ml-1 text-xl" />
                        {{ parent.title }}
                    </RouterLink>
                    <h1
                        id="content-title"
                        class="font-display text-[32px] leading-tight font-semibold [overflow-wrap:anywhere] sm:text-[40px]"
                        :class="{ 'sm:mt-2': !parent }"
                    >
                        {{ content.title }}
                    </h1>
                    <div class="flex flex-wrap justify-center gap-1.5 sm:justify-start">
                        <AChip size="sm">{{ displayContentType(content.type) }}</AChip>
                        <AChip v-if="meta.kind" size="sm">{{ capitalize(meta.kind) }}</AChip>
                        <AChip v-if="meta.status" size="sm">
                            Publication: {{ capitalize(meta.status) }}
                        </AChip>
                        <AChip v-if="language" size="sm">{{ language }}</AChip>
                    </div>
                    <p
                        v-for="link in noticeLinks"
                        :key="link.provider"
                        class="text-fg-muted text-xs"
                    >
                        <template v-if="link.state === 'review'">
                            {{
                                plural(link.candidates.length, 'possible match', 'possible matches')
                            }}
                            on {{ link.label }}
                        </template>
                        <template v-else>Matched automatically on {{ link.label }}</template>
                        ·
                        <button
                            type="button"
                            class="a-focus text-primary rounded-sm font-medium hover:underline"
                            @click="
                                showProviderSearchModal(
                                    content.id,
                                    link,
                                    ownTitle(qMetadata.data.value!)
                                )
                            "
                        >
                            {{ link.state === 'review' ? 'Review' : 'Wrong match?' }}
                        </button>
                    </p>
                </header>

                <div class="flex flex-wrap items-start gap-2">
                    <ContinueReadingButton
                        :content-id="content.id"
                        :type="content.type"
                        class="grow basis-full sm:grow-0 sm:basis-auto"
                    />
                    <ReadingStatusButton
                        :content-id="content.id"
                        class="min-w-0 grow sm:w-48 sm:grow-0"
                    />
                    <AIconButton
                        :icon="IconStar"
                        :pressed-icon="IconStarFilled"
                        label="Starred"
                        variant="tonal"
                        :pressed="isStarred"
                        :loading="mUpdateUserData.isPending.value"
                        :class="{ 'text-star': isStarred }"
                        @click="toggleStar"
                    />
                    <OptionsButton :content-id="content.id" />
                </div>

                <RatingButton
                    :content-id="content.id"
                    class="-mt-3 -ml-1 self-center sm:self-start"
                />

                <div v-if="meta.description" class="flex max-w-[640px] flex-col items-start gap-1">
                    <p
                        :id="descriptionId"
                        ref="descriptionEl"
                        class="text-sm whitespace-pre-line"
                        :class="{ 'line-clamp-4': !showDescription }"
                    >
                        {{ meta.description }}
                    </p>
                    <AButton
                        v-if="clamped || showDescription"
                        variant="text"
                        size="sm"
                        :aria-expanded="showDescription"
                        :aria-controls="descriptionId"
                        @click="showDescription = !showDescription"
                    >
                        {{ showDescription ? 'Show less' : 'Show more' }}
                    </AButton>
                </div>

                <dl v-if="details.length" class="flex max-w-[640px] flex-col text-sm">
                    <div
                        v-for="row in details"
                        :key="row.label"
                        class="border-outline-variant grid grid-cols-[7rem_1fr] gap-4 border-t px-1 py-[11px]"
                    >
                        <dt>{{ row.label }}</dt>
                        <dd class="text-fg-muted min-w-0 [overflow-wrap:anywhere]">
                            <template v-if="row.links">
                                <a
                                    v-for="link in row.links"
                                    :key="link.url"
                                    :href="link.url"
                                    target="_blank"
                                    rel="noopener"
                                    class="a-focus text-primary mr-3 inline-flex items-center gap-1 rounded-sm font-medium hover:underline"
                                >
                                    {{ link.label }}
                                    <AIcon :icon="IconOpenInNew" />
                                    <span class="sr-only">(opens in a new tab)</span>
                                </a>
                            </template>
                            <template v-else>{{ row.value }}</template>
                        </dd>
                    </div>
                </dl>
            </div>
        </div>

        <ADialog v-model:open="showCover" :title="`Cover of ${content.title}`" variant="bare">
            <img
                :src="coverUri ?? undefined"
                :alt="`Cover of ${content.title}`"
                class="rounded-cover max-h-[calc(100dvh-32px)] object-contain"
                @click="showCover = false"
            />
        </ADialog>
    </section>
</template>

<script setup lang="ts">
import { useResizeObserver } from '@vueuse/core'
import { computed, ref, useId, useTemplateRef, watch } from 'vue'
import { RouterLink } from 'vue-router'
import AButton from '@/ui/AButton.vue'
import AChip from '@/ui/AChip.vue'
import ACover from '@/ui/ACover.vue'
import ADialog from '@/ui/ADialog.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import { IconChevronLeft, IconOpenInNew, IconStar, IconStarFilled } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi, coverUrl } from '@/utils/api/content'
import { metadataApi, ownTitle, type MetadataLinkRef } from '@/utils/api/metadata'
import type { Content } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { displayContentType, plural } from '@/utils/misc'
import { lengthSummary, readingSpeed } from '@/utils/readingTime'
import ContinueReadingButton from './components/ContinueReadingButton.vue'
import CoverProgress from './components/CoverProgress.vue'
import OptionsButton from './components/OptionsButton.vue'
import { showProviderSearchModal } from './components/ProviderSearchModal.vue'
import RatingButton from './components/RatingButton.vue'
import ReadingStatusButton from './components/ReadingStatusButton.vue'

const props = defineProps<{
    content: Content
}>()

const qParent = contentApi.useGet(() => props.content.parent_id || undefined)
const parent = qParent.data

const mUpdateUserData = contentApi.useUpdateUserData()
const isStarred = computed(() => props.content.user_data?.starred ?? false)
const toast = useToast()

const showCover = ref(false)

// Admins can correct what matching decided; only series have links.
const qMe = usersApi.useMe()
const qMetadata = metadataApi.useContent(() =>
    qMe.data.value?.permissions.includes('ADMIN') && props.content.type.endsWith('_series')
        ? props.content.id
        : null
)
const noticeLinks = computed(
    () =>
        qMetadata.data.value?.links.filter(
            l => (l.state === 'linked' && l.origin === 'auto') || l.state === 'review'
        ) ?? []
)

const coverUri = computed(() => coverUrl(props.content))

const meta = computed(() => props.content.meta ?? {})
const showDescription = ref(false)
const descriptionId = useId()
const descriptionEl = useTemplateRef('descriptionEl')
/** Whether the collapsed description hides text, so "Show more" has something to show. */
const clamped = ref(false)

function measure() {
    const el = descriptionEl.value
    if (el && !showDescription.value) clamped.value = el.scrollHeight > el.clientHeight
}
useResizeObserver(descriptionEl, measure)
watch(() => meta.value.description, measure, { flush: 'post' })

const details = computed(() => {
    const m = meta.value
    const rows: { label: string; value?: string; links?: MetadataLinkRef[] }[] = []
    if (m.staff?.length) {
        rows.push({
            label: 'Staff',
            value: m.staff.map(s => `${s.name} (${s.role})`).join(', '),
        })
    }
    if (m.publishers?.length) rows.push({ label: 'Publishers', value: m.publishers.join(', ') })
    if (m.publication_date) {
        rows.push({ label: 'Published', value: formatDate(m.publication_date) })
    }
    if (props.content.length) {
        rows.push({
            label: 'Length',
            value: lengthSummary(props.content.length, readingSpeed(qMe.data.value?.preferences)),
        })
    }
    if (m.genres?.length) {
        rows.push({
            label: 'Genres',
            value: m.genres.map(g => capitalize(g.replaceAll('_', ' '))).join(', '),
        })
    }
    if (m.links?.length) rows.push({ label: 'Links', links: m.links })
    return rows
})

const language = computed(() => {
    const code = meta.value.language
    if (!code) return null
    try {
        return new Intl.DisplayNames(undefined, { type: 'language' }).of(code) ?? code
    } catch {
        return code
    }
})

function capitalize(s: string) {
    return s.charAt(0).toUpperCase() + s.slice(1)
}

/** A full date in words; a year or year-month stays as is (a day would be made up). */
function formatDate(value: string) {
    if (/^\d{4}(-\d{2})?$/.test(value)) return value
    const date = new Date(value)
    if (isNaN(date.getTime())) return value
    // Date-only values parse as UTC midnight: format in UTC so the day doesn't shift.
    return date.toLocaleDateString(undefined, {
        dateStyle: 'long',
        timeZone: 'UTC',
    })
}

async function toggleStar() {
    const starred = !isStarred.value
    try {
        await mUpdateUserData.mutateAsync({ contentId: props.content.id, starred })
        toast.show({ message: starred ? 'Starred' : 'Removed the star' })
    } catch {
        toast.show({ message: 'Could not update the star', tone: 'danger' })
    }
}
</script>

<style scoped>
@layer ui {
    .info-backdrop {
        position: absolute;
        z-index: -1;
        inset: -12px -16px auto;
        height: 420px;
        overflow: hidden;
        mask-image: linear-gradient(to bottom, black, transparent);
        pointer-events: none;

        & > div {
            position: absolute;
            inset: 0;
            background-position: center 30%;
            background-size: cover;
            opacity: 0.3;
            filter: blur(48px) saturate(1.4);
            transform: scale(1.2);
        }
    }

    .dark .info-backdrop > div {
        opacity: 0.16;
    }

    @media (width >= 60rem) {
        .info-backdrop {
            inset-inline: -40px;
        }
    }

    .cover-button {
        display: block;
        border-radius: var(--radius-cover);
        cursor: zoom-in;

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: 3px;
        }
    }
}
</style>

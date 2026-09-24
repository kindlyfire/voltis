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
                        <AChip v-if="language" size="sm">{{ language }}</AChip>
                    </div>
                </header>

                <div class="flex flex-wrap items-center gap-2">
                    <ContinueReadingButton
                        :content-id="content.id"
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

                <RatingButton :content-id="content.id" class="-ml-1 self-center sm:self-start" />

                <dl v-if="details.length" class="flex max-w-[640px] flex-col text-sm">
                    <div
                        v-for="row in details"
                        :key="row.label"
                        class="border-outline-variant grid grid-cols-[7rem_1fr] gap-4 border-t px-1 py-[11px]"
                    >
                        <dt>{{ row.label }}</dt>
                        <dd class="text-fg-muted min-w-0 [overflow-wrap:anywhere]">
                            <a
                                v-if="row.href"
                                :href="row.href"
                                target="_blank"
                                rel="noopener"
                                class="a-focus text-primary inline-flex items-center gap-1 rounded-sm font-medium hover:underline"
                            >
                                {{ row.value }}
                                <AIcon :icon="IconOpenInNew" />
                                <span class="sr-only">(opens in a new tab)</span>
                            </a>
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
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import AChip from '@/ui/AChip.vue'
import ACover from '@/ui/ACover.vue'
import ADialog from '@/ui/ADialog.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import { IconChevronLeft, IconOpenInNew, IconStar, IconStarFilled } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import { displayContentType } from '@/utils/misc'
import ContinueReadingButton from './components/ContinueReadingButton.vue'
import CoverProgress from './components/CoverProgress.vue'
import OptionsButton from './components/OptionsButton.vue'
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

const coverUri = computed(() =>
    props.content.cover_uri
        ? `${API_URL}/files/cover/${props.content.id}?v=${props.content.file_mtime}`
        : null
)

const details = computed(() => {
    const meta = props.content.meta
    const rows: { label: string; value: string; href?: string }[] = []
    if (!meta) return rows
    if (meta.staff?.length) {
        rows.push({
            label: 'Staff',
            value: meta.staff.map(s => `${s.name} (${s.role})`).join(', '),
        })
    }
    if (meta.publisher) rows.push({ label: 'Publisher', value: meta.publisher })
    if (meta.publication_date) {
        rows.push({ label: 'Published', value: formatDate(meta.publication_date) })
    }
    if (meta.mangabaka_id) {
        rows.push({
            label: 'Links',
            value: 'MangaBaka',
            href: `https://mangabaka.org/${meta.mangabaka_id}`,
        })
    }
    return rows
})

const language = computed(() => {
    const code = props.content.meta?.language
    if (!code) return null
    try {
        return new Intl.DisplayNames(undefined, { type: 'language' }).of(code) ?? code
    } catch {
        return code
    }
})

/** A full date in words; a year or year-month stays as is (a day would be made up). */
function formatDate(value: string) {
    if (/^\d{4}(-\d{2})?$/.test(value)) return value
    const date = new Date(value)
    if (isNaN(date.getTime())) return value
    // Date-only values parse as UTC midnight: format in UTC so the day doesn't shift.
    return date.toLocaleDateString(undefined, { dateStyle: 'long', timeZone: 'UTC' })
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

<template>
    <section
        v-if="tabs.length"
        :id="CONTENT_TABS_ID"
        class="flex scroll-mt-[calc(var(--layout-top)+1rem)] flex-col gap-2"
        :aria-labelledby="showTabs ? undefined : 'children-heading'"
        :aria-label="showTabs ? 'Contents' : undefined"
    >
        <h2 v-if="!showTabs" id="children-heading" class="font-display text-[30px] font-semibold">
            {{ tabs[0]!.label }}
        </h2>
        <!-- Always wrapped, so the grid isn't remounted when the tabs appear. -->
        <ATabs v-model="tab" :options="showTabs ? tabs : []" label="Contents" shared>
            <ContentInfo v-if="tab === 'info'" :details="details" class="pt-4" />
            <!-- Hidden, not unmounted, on Info: the grid keeps its selection. -->
            <div v-if="isSeries || isBook" v-show="tab !== 'info'">
                <ContentGrid
                    v-if="isSeries"
                    :params="{
                        parent_id: content.id,
                        sort: 'order',
                        sort_order: 'asc',
                        section: gridSection,
                    }"
                    :parent="content"
                    to-read-route
                />
                <BookChaptersList v-else :content="content" />
            </div>
        </ATabs>
    </section>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import ContentGrid from '@/components/ContentGrid/ContentGrid.vue'
import ATabs from '@/ui/ATabs.vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import { useRouteQueryParams } from '@/utils/misc'
import ContentInfo from '../ContentInfo.vue'
import { CONTENT_TABS_ID, useContentDetails } from '../useContentDetails'
import BookChaptersList from './BookChaptersList.vue'

type Section = 'contents' | 'specials'

const props = defineProps<{
    content: Content
}>()

const isSeries = computed(() => props.content.type.includes('series'))
const isBook = computed(() => props.content.type === 'book')
const details = useContentDetails(() => props.content)

const query = useRouteQueryParams({ section: 'contents' as string })
const section = computed((): Section =>
    query.section.value === 'specials' ? 'specials' : 'contents'
)

const specialsBuckets = contentApi.useBuckets(
    () => ({
        parent_id: props.content.id,
        sort: 'order',
        sort_order: 'asc',
        section: 'specials',
    }),
    isSeries
)
// Both count valid children.
const specials = computed(() =>
    specialsBuckets.isError.value ? undefined : specialsBuckets.data.value?.total
)
const hasSpecials = computed(
    () => !!specials.value && specials.value !== props.content.children_count
)

const tabs = computed(() => {
    const list: { label: string; value: Section | 'info' }[] = []
    if (isSeries.value || isBook.value) list.push({ label: 'Contents', value: 'contents' })
    if (hasSpecials.value) list.push({ label: 'Specials', value: 'specials' })
    if (details.value.length) list.push({ label: 'Info', value: 'info' })
    return list
})
const showTabs = computed(() => tabs.value.length > 1)
const tab = computed({
    get: () => tabs.value.find(t => t.value === query.section.value)?.value ?? tabs.value[0]?.value,
    set: (v: Section | 'info' | undefined) => (query.section.value = v ?? 'contents'),
})
// A series without specials lists Contents whatever the URL says; one with only specials, or
// whose count failed, lists everything, with no Specials tab.
const gridSection = computed((): Section | undefined => {
    if (specialsBuckets.isError.value) return undefined
    if (specials.value === undefined || hasSpecials.value) return section.value
    return specials.value ? undefined : 'contents'
})
</script>

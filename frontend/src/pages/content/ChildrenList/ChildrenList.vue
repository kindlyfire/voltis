<template>
  <section
    v-if="isSeries"
    class="flex flex-col gap-2"
    :aria-labelledby="showTabs ? undefined : 'children-heading'"
    :aria-label="showTabs ? 'Contents' : undefined"
  >
    <h2
      v-if="!showTabs"
      id="children-heading"
      class="font-display text-[30px] font-semibold"
    >
      Contents
    </h2>
    <!-- Always wrapped, so the grid isn't remounted when the tabs appear. -->
    <ATabs
      v-model="section"
      :options="showTabs ? TABS : []"
      label="Contents"
      shared
    >
      <ContentGrid
        :params="{
          parent_id: content.id,
          sort: 'order',
          sort_order: 'asc',
          section: gridSection,
        }"
        :parent="content"
        to-read-route
      />
    </ATabs>
  </section>
  <BookChaptersList v-else-if="content.type === 'book'" :content="content" />
</template>

<script lang="ts" setup>
import { computed } from "vue";
import ContentGrid from "@/components/ContentGrid/ContentGrid.vue";
import ATabs from "@/ui/ATabs.vue";
import { contentApi } from "@/utils/api/content";
import type { Content } from "@/utils/api/types";
import { useRouteQueryParams } from "@/utils/misc";
import BookChaptersList from "./BookChaptersList.vue";

type Section = "contents" | "specials";

const TABS = [
  { label: "Contents", value: "contents" },
  { label: "Specials", value: "specials" },
] as const;

const props = defineProps<{
  content: Content;
}>();

const isSeries = computed(() => props.content.type.includes("series"));

const query = useRouteQueryParams({ section: "contents" as string });
const section = computed({
  get: (): Section =>
    query.section.value === "specials" ? "specials" : "contents",
  set: (v: Section) => (query.section.value = v),
});

const specialsBuckets = contentApi.useBuckets(
  () => ({
    parent_id: props.content.id,
    sort: "order",
    sort_order: "asc",
    section: "specials",
  }),
  isSeries,
);
// Both count valid children.
const specials = computed(() =>
  specialsBuckets.isError.value ? undefined : specialsBuckets.data.value?.total,
);
const showTabs = computed(
  () => !!specials.value && specials.value !== props.content.children_count,
);
// A series without specials lists Contents whatever the URL says; one with only specials, or
// whose count failed, lists everything, without tabs.
const gridSection = computed((): Section | undefined => {
  if (specialsBuckets.isError.value) return undefined;
  if (specials.value === undefined || showTabs.value) return section.value;
  return specials.value ? undefined : "contents";
});
</script>

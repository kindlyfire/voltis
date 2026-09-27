<template>
    <ACombobox
        v-model="model"
        v-model:search="search"
        remote
        :options="options"
        :label="label"
        placeholder="Move to…"
        size="sm"
        clearable
        :loading="qTargets.isFetching.value"
        :disabled="disabled"
    >
        <template #option="{ option }">
            {{ option.label }}
            <span v-if="option.description" class="text-fg-muted">· {{ option.description }}</span>
        </template>
    </ACombobox>
</template>

<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { computed, ref } from 'vue'
import ACombobox from '@/ui/ACombobox.vue'
import { contentApi } from '@/utils/api/content'

/** Picks the content an orphan moves to, searched on the server by URI or title. */
const props = defineProps<{
    libraryId: string
    /** Only series, which links attach to. */
    series: boolean
    label: string
    disabled?: boolean
}>()
const model = defineModel<string | null>({ required: true })

const search = ref('')
const debounced = refDebounced(search, 300)
const qTargets = contentApi.useOrphanTargets(
    () => props.libraryId,
    () => ({ search: debounced.value.trim(), series: props.series })
)
const options = computed(() => {
    const found = (qTargets.data.value?.data ?? []).map(t => ({
        value: t.uri,
        label: t.uri,
        description: t.title ?? undefined,
    }))
    // The chosen one keeps its label whatever the search finds.
    const chosen = model.value
    return chosen && !found.some(o => o.value === chosen)
        ? [{ value: chosen, label: chosen, description: undefined }, ...found]
        : found
})
</script>

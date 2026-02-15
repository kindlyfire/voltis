<template>
    <VMenu :close-on-content-click="false">
        <template #activator="{ props: menuProps }">
            <VBtn icon="mdi-tune-variant" size="small" variant="text" v-bind="menuProps" />
        </template>
        <VCard min-width="200">
            <VCardText>
                <div class="d-flex align-center justify-space-between mb-1">
                    <span class="text-caption text-medium-emphasis">Columns</span>
                    <a class="text-caption cursor-pointer" @click="store.resetKey(storeKey)"
                        >Reset</a
                    >
                </div>
                <div class="d-flex align-center ga-2">
                    <VBtn
                        icon="mdi-minus"
                        size="x-small"
                        variant="outlined"
                        :disabled="cols <= minCols"
                        v-bind="minusHold"
                    />
                    <VTextField
                        v-model="colsInput"
                        type="number"
                        density="compact"
                        hide-details
                        class="grow"
                        style="min-width: 150px"
                        @blur="commitColsInput"
                        @keydown.enter="commitColsInput"
                    />
                    <VBtn
                        icon="mdi-plus"
                        size="x-small"
                        variant="outlined"
                        :disabled="cols >= maxCols"
                        v-bind="plusHold"
                    />
                </div>

                <VDivider class="my-3" />

                <div class="d-flex align-center justify-space-between mb-1">
                    <span class="text-caption text-medium-emphasis">Visibility</span>
                    <a class="text-caption cursor-pointer" @click="showAll">Show all</a>
                </div>
                <VCheckbox
                    v-model="hideItemCount"
                    label="Hide item count"
                    density="compact"
                    hide-details
                />
                <VCheckbox
                    v-model="hideStatus"
                    label="Hide reading status"
                    density="compact"
                    hide-details
                />
                <VCheckbox v-model="hideTitle" label="Hide title" density="compact" hide-details />
            </VCardText>
        </VCard>
    </VMenu>
</template>

<script setup lang="ts">
import { ref, computed, watch, toRef } from 'vue'
import { useRepeatOnHold } from '@/utils/misc'
import { useContentGridStore } from './useContentGridStore'

const MIN_ITEM_SIZE = 120
const MAX_ITEM_SIZE = 400

const props = defineProps<{
    storeKey: string
    width: number
}>()

const store = useContentGridStore()
const settings = store.getForKey(toRef(props, 'storeKey'))

const cols = computed({
    get: () => {
        if (props.width <= 0) return 1
        return Math.max(1, Math.round(props.width / settings.value.itemSize))
    },
    set: (n: number) => {
        if (props.width > 0) settings.value = { itemSize: Math.round(props.width / n) }
    },
})

const maxCols = computed(() => {
    if (props.width <= 0) return 1
    return Math.max(1, Math.round(props.width / MIN_ITEM_SIZE))
})

const minCols = computed(() => {
    if (props.width <= 0) return 1
    return Math.max(1, Math.round(props.width / MAX_ITEM_SIZE))
})

const colsInput = ref(String(cols.value))

watch(cols, v => {
    colsInput.value = String(v)
})

watch(colsInput, v => {
    const n = Number(v)
    if (Number.isFinite(n) && n >= minCols.value && n <= maxCols.value) {
        cols.value = n
    }
})

function commitColsInput() {
    const n = Math.max(minCols.value, Math.min(maxCols.value, Math.round(Number(colsInput.value))))
    if (!Number.isFinite(n)) return
    cols.value = n
    colsInput.value = String(cols.value)
}

const minusHold = useRepeatOnHold(() => {
    if (cols.value > minCols.value) cols.value--
})
const plusHold = useRepeatOnHold(() => {
    if (cols.value < maxCols.value) cols.value++
})

function showAll() {
    settings.value = { hideItemCount: false, hideStatus: false, hideTitle: false }
}

const hideItemCount = computed({
    get: () => settings.value.hideItemCount,
    set: (v: boolean) => {
        settings.value = { hideItemCount: v }
    },
})
const hideStatus = computed({
    get: () => settings.value.hideStatus,
    set: (v: boolean) => {
        settings.value = { hideStatus: v }
    },
})
const hideTitle = computed({
    get: () => settings.value.hideTitle,
    set: (v: boolean) => {
        settings.value = { hideTitle: v }
    },
})
</script>

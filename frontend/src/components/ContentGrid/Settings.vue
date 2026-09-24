<template>
    <APopover label="Display options" :width="288">
        <template #trigger>
            <AIconButton :icon="IconTune" label="Display options" />
        </template>

        <div class="flex flex-col gap-3">
            <div class="flex items-center justify-between">
                <h2 class="text-fg-muted text-xs font-semibold tracking-wide">Columns</h2>
                <AButton variant="text" size="sm" class="-my-2 -mr-2" @click="reset">
                    Reset
                </AButton>
            </div>
            <div class="flex items-center gap-2">
                <AIconButton
                    :icon="IconMinus"
                    label="Fewer columns"
                    variant="outlined"
                    :disabled="cols <= minCols"
                    focusable-when-disabled
                    :tooltip="false"
                    v-bind="minusHold"
                />
                <ATextField
                    v-model="colsInput"
                    type="number"
                    label="Columns"
                    size="sm"
                    inputmode="numeric"
                    :autofocus="finePointer"
                    :min="minCols"
                    :max="maxCols"
                    class="min-w-0 flex-1"
                    @blur="commitColsInput"
                    @keydown.enter="commitColsInput"
                />
                <AIconButton
                    :icon="IconPlus"
                    label="More columns"
                    variant="outlined"
                    :disabled="cols >= maxCols"
                    focusable-when-disabled
                    :tooltip="false"
                    v-bind="plusHold"
                />
            </div>
        </div>

        <ADivider class="my-4" />

        <div class="flex items-center justify-between">
            <h2 class="text-fg-muted text-xs font-semibold tracking-wide">Visibility</h2>
            <AButton variant="text" size="sm" class="-my-2 -mr-2" @click="showAll">
                Show all
            </AButton>
        </div>
        <div class="flex flex-col">
            <ACheckbox v-model="hideItemCount" label="Hide item count" />
            <ACheckbox v-model="hideStatus" label="Hide reading status" />
            <ACheckbox v-model="hideTitle" label="Hide title" />
            <ACheckbox v-model="hideProgress" label="Hide progress" />
        </div>

        <ADivider class="my-4" />

        <div class="flex flex-col gap-2">
            <h2 class="text-fg-muted text-xs font-semibold tracking-wide">Item count</h2>
            <ASegmented
                v-model="itemCountMode"
                :options="itemCountOptions"
                label="Item count"
                size="sm"
            />
        </div>
    </APopover>
</template>

<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import { ref, computed, watch, toRef } from 'vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADivider from '@/ui/ADivider.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APopover from '@/ui/APopover.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconMinus, IconPlus, IconTune } from '@/ui/icons'
import { usePressAndHold } from '@/ui/usePressAndHold'
import { useContentGridStore, type GridSettings } from './store'

const MIN_ITEM_SIZE = 120
const MAX_ITEM_SIZE = 400

const props = defineProps<{
    storeKey: string
    width: number
}>()

const store = useContentGridStore()
const settings = store.getForKey(toRef(props, 'storeKey'))
// Autofocusing the number field on touch would pop up the on-screen keyboard.
const finePointer = useMediaQuery('(pointer: fine)')

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

const colsInput = ref<number | null>(cols.value)

watch(cols, v => {
    colsInput.value = v
})

watch(colsInput, n => {
    if (n != null && Number.isInteger(n) && n >= minCols.value && n <= maxCols.value) {
        cols.value = n
    }
})

function commitColsInput() {
    if (colsInput.value != null) {
        cols.value = Math.max(minCols.value, Math.min(maxCols.value, Math.round(colsInput.value)))
    }
    colsInput.value = cols.value
}

function reset() {
    store.resetKey(props.storeKey)
}

const itemCountOptions = [
    { value: 'unread', label: 'Unread' },
    { value: 'total', label: 'Total' },
] as const

const minusHold = usePressAndHold(() => {
    if (cols.value > minCols.value) cols.value--
})
const plusHold = usePressAndHold(() => {
    if (cols.value < maxCols.value) cols.value++
})

function showAll() {
    settings.value = {
        hideItemCount: false,
        hideStatus: false,
        hideTitle: false,
        hideProgress: false,
        itemCountMode: 'unread',
    }
}

/** A writable ref to one setting. */
function setting<K extends keyof GridSettings>(key: K) {
    return computed({
        get: () => settings.value[key],
        set: (v: GridSettings[K]) => {
            settings.value = { [key]: v }
        },
    })
}

const hideItemCount = setting('hideItemCount')
const itemCountMode = setting('itemCountMode')
const hideStatus = setting('hideStatus')
const hideTitle = setting('hideTitle')
const hideProgress = setting('hideProgress')
</script>

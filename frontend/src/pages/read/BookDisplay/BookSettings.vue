<template>
    <div class="space-y-4">
        <div>
            <div class="text-fg-muted mb-2 text-sm" aria-hidden="true">Layout</div>
            <ASegmented v-model="settings.mode" :options="MODE_OPTIONS" label="Layout" block />
        </div>
        <div v-if="settings.mode === 'paged'">
            <div class="text-fg-muted mb-2 text-sm" aria-hidden="true">Pages per screen</div>
            <ASegmented
                v-model="settings.spread"
                :options="SPREAD_OPTIONS"
                label="Pages per screen"
                block
            />
            <div v-if="autoSpread" class="text-fg-muted mt-1 text-xs">
                Auto: {{ autoSpread === 2 ? 'two pages' : 'one page' }}
            </div>
        </div>
        <ASelect
            :model-value="settings.fontFamily"
            :options="FONT_OPTIONS"
            label="Font"
            @update:model-value="v => v && (settings.fontFamily = v)"
        />
        <ASlider
            v-model="settings.fontSize"
            label="Text size"
            show-value
            :format-value="v => `${v.toFixed(2)} rem`"
            :min="0.7"
            :max="2.5"
            :step="0.05"
        />
        <ASlider
            v-model="settings.lineHeight"
            label="Line height"
            show-value
            :format-value="v => v.toFixed(1)"
            :min="1.1"
            :max="2.5"
            :step="0.1"
        />
        <ASlider
            v-model="settings.width"
            label="Text width"
            show-value
            :format-value="v => `${v} em`"
            :min="20"
            :max="80"
        />
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASelect from '@/ui/ASelect.vue'
import ASlider from '@/ui/ASlider.vue'
import { FONT_OPTIONS, MODE_OPTIONS, SPREAD_OPTIONS } from './bookSettings'
import { useBookDisplayStore } from './useBookDisplayStore'

const store = useBookDisplayStore()
const settings = store.settings
/** What auto comes to at the current width, once a paged chapter is shown. */
const autoSpread = computed(() =>
    settings.spread === 'auto' ? store.session?.screen?.spread : undefined
)
</script>

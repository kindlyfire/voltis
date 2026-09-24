<template>
    <div class="space-y-4">
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
import ASelect from '@/ui/ASelect.vue'
import ASlider from '@/ui/ASlider.vue'
import { FONT_OPTIONS } from './bookSettings'
import { useBookDisplayStore } from './useBookDisplayStore'

const settings = useBookDisplayStore().settings
</script>

<template>
    <div class="space-y-4">
        <div>
            <div class="mb-1 text-sm opacity-60">Font</div>
            <VSelect
                v-model="settings.fontFamily"
                :items="FONT_OPTIONS"
                density="compact"
                hide-details
            />
        </div>

        <div>
            <div class="mb-1 text-sm opacity-60">
                Text size: {{ settings.fontSize.toFixed(2) }}rem
            </div>
            <VSlider v-model="settings.fontSize" :min="0.7" :max="2.5" :step="0.05" hide-details />
        </div>

        <div>
            <div class="mb-1 text-sm opacity-60">
                Line height: {{ settings.lineHeight.toFixed(1) }}
            </div>
            <VSlider v-model="settings.lineHeight" :min="1.1" :max="2.5" :step="0.1" hide-details />
        </div>

        <div>
            <div class="mb-1 text-sm opacity-60">Text width: {{ settings.width }}em</div>
            <VSlider v-model="settings.width" :min="20" :max="80" :step="1" hide-details />
        </div>
    </div>
</template>

<script setup lang="ts">
import { FONT_OPTIONS } from './bookSettings'
import { useBookDisplayStore } from './useBookDisplayStore'

const settings = useBookDisplayStore().settings
</script>

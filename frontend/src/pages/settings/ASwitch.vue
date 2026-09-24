<template>
    <VSwitch
        :model-value="modelValue"
        @update:model-value="emit('update:modelValue', $event ?? false)"
        :label="label"
        color="primary"
        density="compact"
        hide-details
    />
</template>

<script setup lang="ts">
defineProps<{
    modelValue: boolean
    label: string
}>()

const emit = defineEmits<{ 'update:modelValue': [boolean] }>()
</script>

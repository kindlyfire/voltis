<template>
	<VTextarea
		v-if="type === 'textarea'"
		:model-value="input.modelValue"
		@update:model-value="input['onUpdate:modelValue']($event)"
		@blur="input.onBlur"
		:error-messages="errorMessages"
		:hide-details="!errorMessages.length"
		v-bind="$attrs"
	/>
	<VTextField
		v-else
		:type="type"
		:model-value="input.modelValue"
		@update:model-value="input['onUpdate:modelValue']($event)"
		@blur="input.onBlur"
		:error-messages="errorMessages"
		:hide-details="!errorMessages.length"
		v-bind="$attrs"
	/>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface InputProps {
	modelValue: unknown
	'onUpdate:modelValue': (value: unknown) => void
	onBlur: () => void
	errors: { message: string }[]
	isTouched: boolean
	isDirty: boolean
}

const props = withDefaults(
	defineProps<{
		input: InputProps
		type?: 'text' | 'password' | 'email' | 'number' | 'textarea'
	}>(),
	{ type: 'text' }
)

const errorMessages = computed(() => props.input.errors.map(e => e.message))
</script>

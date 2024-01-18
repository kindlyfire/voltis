<template>
	<ReaderMain />
</template>

<script lang="ts" setup>
import ReaderMain from './ReaderMain.vue'
import type { ReaderProvider } from './types'
import { readerKey, useReader } from './use-reader'

const props = defineProps<{
	provider: ReaderProvider
}>()

const reader = useReader(props.provider)
provide(readerKey, reader)

onMounted(() => {
	reader.state.scrollRef = document.getElementById('mainOverflowArea')
})

defineExpose({
	reader
})
</script>

<style></style>

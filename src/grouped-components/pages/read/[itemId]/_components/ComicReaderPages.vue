<template>
	<template v-for="p in store.readerPages.value">
		<div
			v-if="p.error || !p.blobUrl"
			:style="
				store.readerMode.value === 'longstrip' && {
					width: p.file.width + 'px',
					height: p.file.height + 'px'
				}
			"
			class="flex flex-col items-center justify-center gap-2"
		>
			<template v-if="p.error">
				<div>
					{{ p.error }}
				</div>
				<div>
					<UButton @click="p.fetch">Retry</UButton>
				</div>
			</template>
			<div v-else>
				<UIcon
					name="ph:circle-dashed-bold"
					dynamic
					class="h-10 w-10 animate-spin"
				/>
			</div>
		</div>
		<img
			v-else
			:src="p.blobUrl"
			alt=""
			:class="modeClasses[store.readerMode.value].images"
		/>
	</template>
</template>

<script lang="ts" setup>
import { readerStateKey } from '../state'
import { modeClasses } from './shared'

const store = inject(readerStateKey)!
</script>

<style></style>

<template>
	<div :class="LIBRARY_GRID_CLASSES">
		<template v-if="loading">
			<AContentGridItemSkeleton />
			<AContentGridItemSkeleton />
			<AContentGridItemSkeleton />
		</template>

		<template v-else>
			<AContentGridItem
				v-for="item in items"
				:key="item.id"
				:to="`/${item.id}`"
				:title="item.title"
				:cover-uri="
					item.cover_uri ? `${API_URL}/files/cover/${item.id}?v=${item.file_mtime}` : null
				"
				:children-count="item.type === 'book_series' ? item.children_count : null"
			/>
		</template>
	</div>
</template>

<script setup lang="ts">
import type { Content } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import { LIBRARY_GRID_CLASSES } from '@/utils/misc'
import AContentGridItemSkeleton from './AContentGridItemSkeleton.vue'
import AContentGridItem from './AContentGridItem.vue'

const props = defineProps<{
	items: Content[]
	loading: boolean
}>()
</script>

<template>
	<VContainer>
		<div class="space-y-8">
			<section v-if="lastRead?.length">
				<ACarousel title="Last Read">
					<template v-if="qLastRead.isLoading.value">
						<ACarouselItem v-for="i in 3" :key="i">
							<AContentGridItemSkeleton />
						</ACarouselItem>
					</template>
					<template v-else>
						<ACarouselItem v-for="item in lastRead" :key="item.id">
							<AContentGridItem
								:to="`/${item.id}?page=resume`"
								:id="item.id"
								:title="item.title"
								:cover-uri="
									item.cover_uri
										? `${API_URL}/files/cover/${item.id}?v=${item.file_mtime}`
										: null
								"
								:children-count="
									item.type === 'book_series' || item.type === 'comic_series'
										? item.children_count
										: null
								"
								:user-data="item.user_data ?? null"
							/>
						</ACarouselItem>
					</template>
				</ACarousel>
			</section>

			<section class="mt-4">
				<ACarousel title="Newest">
					<template v-if="qNewest.isLoading.value">
						<ACarouselItem v-for="i in 3" :key="i">
							<AContentGridItemSkeleton />
						</ACarouselItem>
					</template>
					<template v-else>
						<ACarouselItem v-for="item in newest?.data ?? []" :key="item.id">
							<AContentGridItem
								:to="`/${item.id}?page=resume`"
								:id="item.id"
								:title="item.title"
								:cover-uri="
									item.cover_uri
										? `${API_URL}/files/cover/${item.id}?v=${item.file_mtime}`
										: null
								"
								:children-count="
									item.type === 'book_series' || item.type === 'comic_series'
										? item.children_count
										: null
								"
								:user-data="item.user_data ?? null"
							/>
						</ACarouselItem>
					</template>
				</ACarousel>
			</section>
		</div>
	</VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useHead } from '@unhead/vue'
import ACarousel from '@/components/ACarousel.vue'
import ACarouselItem from '@/components/ACarouselItem.vue'
import AContentGridItem from '@/components/AContentGridItem.vue'
import AContentGridItemSkeleton from '@/components/AContentGridItemSkeleton.vue'
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'

useHead({
	title: 'Home',
})

const qLastRead = contentApi.useList({
	reading_status: 'reading',
	sort: 'progress_updated_at',
	sort_order: 'desc',
	type: ['book', 'comic'],
	limit: 10,
})
const lastRead = computed(() => qLastRead.data.value?.data ?? [])

const qNewest = contentApi.useList({
	parent_id: 'null',
	sort: 'created_at',
	sort_order: 'desc',
	limit: 10,
})
const newest = computed(() => qNewest.data.value)
</script>

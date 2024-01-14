<template>
	<div class="acontainer">
		<div
			class="grid grid-cols-2 min-[400px]:grid-cols-3 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8 gap-2"
		>
			<NuxtLink
				:to="'/' + slugify(i.name) + ':' + i.id"
				v-for="i in items.data.value"
				class="card p-0 overflow-hidden"
				:title="i.name"
			>
				<img class="cover" :src="'/api/cover?collection-id=' + i.id" alt="" />
				<div
					class="overflow-hidden whitespace-nowrap text-ellipsis w-full text-sm px-1"
				>
					{{ i.name }}
				</div>
			</NuxtLink>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { trpc } from '../plugins/trpc'
import slugify from 'slugify'

const items = trpc.collections.query.useLazyQuery({})
</script>

<style>
img.cover {
	width: 100%;
	aspect-ratio: 158/230;
	object-fit: cover;
}
</style>

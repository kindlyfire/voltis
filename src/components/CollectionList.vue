<template>
	<GridContainer :items="collections" v-slot="{ item: i }">
		<NuxtLink
			:to="routeBuilder['/c/[collectionId]/[name]'](i.id, slugify(i.name))"
			class="card p-0 overflow-hidden"
			:title="i.name"
		>
			<img
				class="cover"
				:src="'/api/cover?width=320&collection-id=' + i.id"
				alt=""
			/>
			<div
				class="overflow-hidden whitespace-nowrap text-ellipsis w-full text-sm px-1"
			>
				{{ i.name }}
			</div>
		</NuxtLink>
	</GridContainer>
</template>

<script lang="ts" setup>
import type { Collection } from '@prisma/client'
import GridContainer from './GridContainer.vue'
import slugify from 'slugify'

const props = defineProps<{
	collections: Collection[]
}>()
const emit = defineEmits<{}>()
</script>

<style></style>

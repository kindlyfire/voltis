<template>
	<div>
		<div class="h-[50px] flex items-center px-2 gap-1 sm:gap-2">
			<UButton color="gray" @click="store.menuOpen = false">
				<UIcon name="ph:x-bold" dynamic class="h-5 scale-[1.2]" />
			</UButton>
		</div>
		<div class="p-2 flex flex-col gap-2">
			<div class="flex items-center gap-2 pl-2">
				<div>
					<UIcon name="ph:book-open-bold" dynamic class="h-6 w-6" />
				</div>
				<div class="text-sm">
					{{ store.collection?.name }}
				</div>
			</div>

			<div class="flex items-center gap-2 pl-2">
				<div>
					<UIcon name="ph:file-bold" dynamic class="h-6 w-6" />
				</div>
				<div class="text-sm">
					{{ store.item?.name }}
				</div>
			</div>

			<UFormGroup label="Page">
				<div class="flex items-center gap-1">
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="store.switchPage(-1)"
					>
						<UIcon name="ph:caret-left-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
					<USelectMenu
						class="grow"
						:options="pageOptions"
						value-attribute="value"
						option-attribute="label"
						v-model="store.readerState.pageIndex"
					>
						<template #label>
							Page {{ store.readerState.pageIndex + 1 }}
						</template>
					</USelectMenu>
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="store.switchPage(1)"
					>
						<UIcon name="ph:caret-right-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
				</div>
			</UFormGroup>

			<UFormGroup label="Chapter">
				<div class="flex items-center gap-1">
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="store.switchChapter(-1)"
					>
						<UIcon name="ph:caret-left-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
					<USelectMenu
						class="grow"
						:options="chapterOptions"
						value-attribute="value"
						option-attribute="label"
						:model-value="store.itemId ?? ''"
						@update:model-value="$router.push('/read/' + $event)"
					>
						<template #label>
							{{ store.item?.name || '...' }}
						</template>
					</USelectMenu>
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="store.switchChapter(1)"
					>
						<UIcon name="ph:caret-right-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
				</div>
			</UFormGroup>

			<hr />

			<UButton @click="store.switchMode()" size="lg" color="gray">
				{{ store.readerMode === 'pages' ? 'Single Page' : 'Longstrip' }}
			</UButton>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { useComicReaderStore } from '../state'

const store = useComicReaderStore()

const pageOptions = computed(() =>
	store.readerState.pages.map((page, index) => ({
		label: `Page ${index + 1}`,
		value: index
	}))
)

const chapterOptions = computed(
	() =>
		store.items?.map(item => ({
			label: item.name,
			value: item.id
		})) ?? []
)
</script>

<style></style>

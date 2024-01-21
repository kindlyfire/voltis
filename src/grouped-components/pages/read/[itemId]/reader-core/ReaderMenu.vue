<template>
	<div>
		<div class="h-[50px] flex items-center px-2 gap-1 sm:gap-2">
			<UButton color="gray" @click="reader.state.menuOpen = false">
				<UIcon name="ph:x-bold" dynamic class="h-5 scale-[1.2]" />
			</UButton>
		</div>
		<div class="p-2 flex flex-col gap-2">
			<div class="flex items-center gap-2 pl-2">
				<div>
					<UIcon name="ph:book-open-bold" dynamic class="h-6 w-6" />
				</div>
				<NuxtLink
					class="text-sm text-primary hover:underline"
					:to="chapter?.collection.link"
				>
					{{ chapter?.collection.title ?? 'Loading...' }}
				</NuxtLink>
			</div>

			<div class="flex items-center gap-2 pl-2">
				<div>
					<UIcon name="ph:file-bold" dynamic class="h-6 w-6" />
				</div>
				<div class="text-sm">
					{{ chapter?.title ?? 'Loading...' }}
				</div>
			</div>

			<UFormGroup
				:ui="{
					label: {
						base: 'w-full'
					}
				}"
			>
				<template #label>
					<div class="flex items-center mb-1 w-full">
						<div>Page</div>
						<div class="ml-auto">
							{{
								reader.activeChapter.value
									? `(${reader.activeChapter.value.pages.length})`
									: ''
							}}
						</div>
					</div>
				</template>

				<div class="flex items-center gap-1">
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="reader.goToPage(reader.state.page - 1)"
					>
						<UIcon name="ph:caret-left-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
					<USelectMenu
						class="grow"
						:options="pageOptions"
						value-attribute="value"
						option-attribute="label"
						:model-value="reader.state.page"
						@update:model-value="reader.goToPage($event)"
					>
						<template #label> Page {{ reader.state.page + 1 }} </template>
					</USelectMenu>
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="reader.goToPage(reader.state.page + 1)"
					>
						<UIcon name="ph:caret-right-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
				</div>
			</UFormGroup>

			<UFormGroup
				:ui="{
					label: {
						base: 'w-full'
					}
				}"
			>
				<template #label>
					<div class="flex items-center mb-1 w-full">
						<div>Chapter</div>
						<div class="ml-auto">
							{{
								reader.state.chapters.length
									? `(${reader.state.chapters.length})`
									: ''
							}}
						</div>
					</div>
				</template>

				<div class="flex items-center gap-1">
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="reader.switchChapter(SwitchChapterDirection.Backward)"
					>
						<UIcon name="ph:caret-left-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
					<USelectMenu
						class="grow"
						:options="chapterOptions"
						value-attribute="value"
						option-attribute="label"
						:model-value="reader.state.chapterId"
						@update:model-value="reader.switchChapterById($event)"
					>
						<template #label>
							{{ chapter?.title ?? 'Loading...' }}
						</template>
					</USelectMenu>
					<UButton
						square
						class="w-[32px] justify-center"
						color="gray"
						@click="reader.switchChapter(SwitchChapterDirection.Forward)"
					>
						<UIcon name="ph:caret-right-bold" dynamic class="h-5 scale-[1.2]" />
					</UButton>
				</div>
			</UFormGroup>

			<hr />

			<UButton @click="reader.switchMode()" size="lg" color="gray">
				{{ reader.state.mode === 'pages' ? 'Single Page' : 'Longstrip' }}
			</UButton>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { SwitchChapterDirection } from './types'
import { readerKey } from './use-reader'

const reader = inject(readerKey)!

const chapter = computed(() => {
	return reader.state.chaptersData.find(c => c.id === reader.state.chapterId)
})

const pageOptions = computed(() => {
	return (
		chapter.value?.pages.map((_, i) => ({
			label: `Page ${i + 1}`,
			value: i
		})) ?? []
	)
})
const chapterOptions = computed(() => {
	return reader.state.chapters.map(c => ({
		label: c.title,
		value: c.id
	}))
})
</script>

<style></style>

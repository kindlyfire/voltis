<template>
	<div class="flex flex-col gap-1">
		<div
			v-for="i in pageItems"
			class="card w-full border-l-4 cursor-pointer"
			:class="[
				i.userData?.completed
					? 'border-l-gray-300'
					: 'border-l-[rgb(var(--color-primary-DEFAULT)/0.75)]'
			]"
			@click="
				$router.push('/read/' + i.id + '/' + (i.userData?.progress?.page ?? 0))
			"
		>
			<div class="flex items-center gap-2">
				<div>
					<button
						class="flex items-center text-muted -m-2 h-10 px-2"
						@click.stop.prevent="
							updateItem({ id: i.id, completed: !i.userData?.completed })
						"
					>
						<UIcon
							v-if="i.userData?.completed"
							name="ph:eye-slash-bold"
							dynamic
							class="scale-[1.2] opacity-25"
						/>
						<UIcon v-else name="ph:eye-bold" dynamic class="scale-[1.2]" />
					</button>
				</div>
				<NuxtLink
					class="overflow-hidden whitespace-nowrap text-ellipsis font-semibold grow"
					:to="'/read/' + i.id + '/' + (i.userData?.progress?.page ?? 0)"
				>
					{{ i.name }}

					<span
						class="text-primary font-normal text-sm"
						v-if="i.userData?.progress"
					>
						Page {{ i.userData.progress.page + 1 }}
					</span>
				</NuxtLink>
			</div>
		</div>
	</div>
	<div class="flex items-center justify-center">
		<UPagination
			:page-count="pageSize"
			:total="items?.length ?? 0"
			v-model="page"
			show-last
			show-first
			size="lg"
		/>
	</div>
</template>

<script lang="ts" setup>
import { trpc } from '../../../../plugins/trpc'
import { useUser, type useItems } from '../../../../state/composables/queries'

const props = defineProps<{
	qItems: ReturnType<typeof useItems>
}>()
const emit = defineEmits<{}>()
const user = useUser()

const items = computed(() => props.qItems.data?.value ?? [])

const page = ref(1)
const pageSize = ref(50)
const pageItems = computed(() => {
	const start = (page.value - 1) * pageSize.value
	const end = start + pageSize.value
	return items.value.slice(start, end)
})

const itemChangePromises = reactive(new Map<string, Promise<any>>())
function updateItem(data: {
	id: string
	completed?: boolean
	bookmarked?: boolean
}) {
	if (itemChangePromises.has(data.id) || !user.data.value) {
		return
	}

	const p = Promise.resolve()
		.then(async () => {
			await trpc.items.updateUserData.mutate({
				itemId: data.id,
				completed: data.completed,
				bookmarked: data.bookmarked
			})
			await props.qItems.refetch()
		})
		.catch(e => {
			console.error('Error updating item', e)
		})
		.finally(() => {
			itemChangePromises.delete(data.id)
		})
	itemChangePromises.set(data.id, p)
}
</script>

<style></style>

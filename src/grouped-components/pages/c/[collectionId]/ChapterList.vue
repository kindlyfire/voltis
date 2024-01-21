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
						:disabled="itemChangePromises.has(i.id)"
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
				<div v-if="user">
					<UPopover
						:popper="{ placement: 'bottom-end', offsetDistance: 4 }"
						class="flex items-center justify-center"
					>
						<button class="flex items-center justify-center -m-2 h-10 px-2">
							<UIcon
								name="ph:dots-three-vertical-bold"
								dynamic
								class="scale-[1.2]"
							/>
						</button>

						<template #panel>
							<div
								class="p-1 w-[10rem] flex flex-col cursor-auto"
								@click.stop.prevent=""
							>
								<div class="px-2.5 py-1 text-sm text-muted">Mark below</div>
								<div class="grid grid-cols-2 gap-1">
									<UButton
										color="gray"
										class="justify-center"
										size="xs"
										@click.stop.prevent="
											updateItemBulk({
												id: i.id,
												completed: true,
												direction: 'below'
											})
										"
									>
										Read
									</UButton>
									<UButton
										color="gray"
										class="justify-center"
										size="xs"
										@click.stop.prevent="
											updateItemBulk({
												id: i.id,
												completed: false,
												direction: 'below'
											})
										"
									>
										Unread
									</UButton>
								</div>
								<div class="px-2.5 py-1 text-sm text-muted">Mark above</div>
								<div class="grid grid-cols-2 gap-1">
									<UButton
										color="gray"
										class="justify-center"
										size="xs"
										@click.stop.prevent="
											updateItemBulk({
												id: i.id,
												completed: true,
												direction: 'above'
											})
										"
									>
										Read
									</UButton>
									<UButton
										color="gray"
										class="justify-center"
										size="xs"
										@click.stop.prevent="
											updateItemBulk({
												id: i.id,
												completed: false,
												direction: 'above'
											})
										"
									>
										Unread
									</UButton>
								</div>
							</div>
						</template>
					</UPopover>
				</div>
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
const qUser = useUser()
const user = qUser.data
const loadingIndicator = useLoadingIndicator()

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
	if (itemChangePromises.has(data.id) || !user.value) {
		return
	}

	loadingIndicator.start()
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
			loadingIndicator.finish()
		})
	itemChangePromises.set(data.id, p)
}

function updateItemBulk(data: {
	id: string
	completed: boolean
	direction: 'above' | 'below'
}) {
	if (itemChangePromises.has(data.id) || !user.value) {
		return
	}

	const itemIndex = items.value.findIndex(i => i.id === data.id)
	if (itemIndex === -1) return

	const itemsToChange =
		data.direction === 'above'
			? items.value.slice(0, itemIndex)
			: items.value.slice(itemIndex + 1)

	if (itemsToChange.length === 0) return

	loadingIndicator.start()
	const p = Promise.resolve()
		.then(async () => {
			await trpc.items.bulkUpdateReadStatus.mutate({
				itemIds: itemsToChange.map(i => i.id),
				completed: data.completed
			})
			await props.qItems.refetch()
		})
		.catch(e => {
			console.error('Error updating items', e)
		})
		.finally(() => {
			itemChangePromises.delete(data.id)
			loadingIndicator.finish()
		})
	itemChangePromises.set(data.id, p)
}
</script>

<style></style>

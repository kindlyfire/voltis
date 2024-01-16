<template>
	<div
		class="card px-0 rounded-none border-t-0 border-x-0 mb-4 flex flex-row items-center h-[50px] shrink-0"
	>
		<div class="flex flex-row items-center gap-1 sm:gap-2 acontainer">
			<UButton color="gray" class="wide:hidden">
				<UIcon
					name="ph:list-bold"
					dynamic
					class="h-5 scale-[1.2]"
					@click="layoutStore.sidebarOpen = true"
				/>
			</UButton>
			<NuxtLink
				to="/"
				class="font-bold text-lg font-mono"
				:class="[sidebarEnabled ? 'block wide:hidden' : '']"
				>Voltis</NuxtLink
			>
			<div class="ml-auto"></div>

			<template v-if="user">
				<UButton color="gray" @click="searchModalOpen = true">
					<UIcon
						name="ph:magnifying-glass-bold"
						dynamic
						class="h-5 scale-[1.2]"
					/>
					<div class="hidden sm:block">Search</div>
				</UButton>

				<ClientOnly>
					<template #fallback>
						<UButton color="gray">
							<UIcon
								name="ph:caret-down-bold"
								dynamic
								class="scale-[1.2] h-5"
							/>
						</UButton>
					</template>

					<UPopover :popper="{ placement: 'bottom-end', offsetDistance: 4 }">
						<UButton color="gray">
							{{ user.username }}
							<UIcon
								name="ph:caret-down-bold"
								dynamic
								class="scale-[1.2] h-5"
							/>
						</UButton>

						<template #panel>
							<div class="p-1 w-[10rem] flex flex-col">
								<UButton to="/user/account" variant="ghost" color="gray"
									>My account</UButton
								>
								<UButton
									v-if="user.roles?.includes('admin')"
									to="/admin/summary"
									variant="ghost"
									color="gray"
									>Admin dashboard</UButton
								>
								<hr class="my-1" />
								<UButton
									@click="mLogout.mutate()"
									:loading="mLogout.isPending.value"
									variant="ghost"
									color="red"
								>
									Log out
								</UButton>
							</div>
						</template>
					</UPopover>
				</ClientOnly>
			</template>
			<template v-else>
				<UButton to="/auth/login" color="gray">Log in</UButton>
				<UButton
					v-if="qMeta.data.value?.registrationsEnabled"
					to="/auth/register"
					color="gray"
					>Register</UButton
				>
			</template>
		</div>

		<SearchModal v-model="searchModalOpen" />
	</div>
</template>

<script lang="ts" setup>
import { useMutation } from '@tanstack/vue-query'
import { trpc } from '../../../plugins/trpc'
import { useMeta, useUser } from '../../../state/composables/queries'
import { useLayoutStore } from '../state'

const route = useRoute()
const sidebarEnabled = computed(() => route.meta.sidebarEnabled ?? true)
const qMeta = useMeta()
const qUser = useUser()
const user = qUser.data
const searchModalOpen = ref(false)
const layoutStore = useLayoutStore()

const mLogout = useMutation({
	async mutationFn() {
		await trpc.auth.logout.mutate()
		await qUser.refetch()
	}
})
</script>

<style></style>

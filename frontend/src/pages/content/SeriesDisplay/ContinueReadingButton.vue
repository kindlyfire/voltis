<template>
	<VBtn size="large" class="h-12!" :disabled="nextStatus == null" @click="onClick">
		<template
			v-if="nextStatus == null || nextStatus === 'all-completed' || nextStatus == 'starting'"
		>
			Start Reading
		</template>
		<template v-else>Continue Reading</template>
	</VBtn>
	<ResetReadingModal v-model="showResetModal" :content-id="props.contentId" />
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { useKeyModifier } from '@vueuse/core'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import ResetReadingModal from './ResetReadingModal.vue'

const props = defineProps<{
	contentId: string
}>()

const showResetModal = ref(false)

const router = useRouter()
const qContent = contentApi.useGet(() => props.contentId)
const qChildren = contentApi.useList(() => ({ parent_id: props.contentId, sort: true }))

const nextStatus = computed(() => {
	const children = qChildren.data.value ?? []
	if (!children.length || !qChildren.data.value) return null

	const firstUnread = children.findIndex(child => {
		return child.user_data?.status !== 'completed'
	})
	if (firstUnread === -1) {
		return 'all-completed'
	} else if (firstUnread === 0 && children[firstUnread]!.user_data?.status != 'reading') {
		return 'starting'
	} else {
		return 'resume'
	}
})

const ctrlModifier = useKeyModifier('Control')

function onClick() {
	const ns = nextStatus.value
	if (ns == null) return

	if (ns === 'all-completed') {
		showResetModal.value = true
		return
	}

	const firstUnread = qChildren.data.value!.find(child => {
		return child.user_data?.status !== 'completed'
	})
	if (!firstUnread) return

	const userData = qContent.data.value?.user_data
	if (!userData?.status) {
		contentApi.updateUserData(props.contentId, {
			status: 'reading',
		})
	}

	if (ctrlModifier.value) {
		window.open(`/${firstUnread.id}?page=resume`, '_blank')
	} else {
		router.push({
			path: `/${firstUnread.id}`,
			query: {
				page: 'resume',
			},
		})
	}
}
</script>

<template>
    <div class="d-flex align-center" @mouseleave="hoverRating = null">
        <VBtn
            v-for="star in 5"
            :key="star"
            :icon="getStarIcon(star)"
            flat
            density="compact"
            @mouseenter="hoverRating = star"
            @click="setRating(star)"
        />
        <VBtn
            v-if="currentRating || mUpdateUserData.isPending.value"
            icon="mdi-close"
            flat
            density="compact"
            class="ml-1"
            :loading="mUpdateUserData.isPending.value"
            @click="setRating(null)"
        />
    </div>
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { computed, ref } from 'vue'

const props = defineProps<{
    contentId: string | null | undefined
}>()

const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data

const currentRating = computed(() => content.value?.user_data?.rating ?? null)
const hoverRating = ref<number | null>(null)
const mUpdateUserData = contentApi.useUpdateUserData()

function getStarIcon(star: number): string {
    const activeRating = hoverRating.value ?? currentRating.value ?? 0
    return star <= activeRating ? 'mdi-star' : 'mdi-star-outline'
}

async function setRating(rating: number | null) {
    if (!content.value) return
    await mUpdateUserData.mutateAsync({ contentId: content.value.id, rating })
    await qContent.refetch()
}
</script>

<template>
    <VContainer v-if="qContent.error.value">
        <AQueryError :query="qContent" />
    </VContainer>
    <div
        v-else-if="qContent.isLoading.value"
        class="absolute inset-0 flex items-center justify-center"
    >
        <VProgressCircular indeterminate size="64" />
    </div>
    <template v-else>
        <InfoHeader :content="qContent.data.value!" />
        <ChildrenList :content="qContent.data.value!" />
    </template>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import ChildrenList from './ChildrenList/ChildrenList.vue'
import InfoHeader from './InfoHeader.vue'

const route = useRoute()
const contentId = computed(() => route.params.id as string)
const qContent = contentApi.useGet(contentId)

useHead({
    title() {
        return qContent.data.value?.title ?? null
    },
})
</script>

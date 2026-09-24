<template>
    <div class="page-frame">
        <QueryError v-if="qContent.error.value" :query="qContent" />
        <div v-else-if="qContent.isLoading.value" class="flex justify-center py-24">
            <ASpinner size="lg" />
        </div>
        <div v-else class="flex flex-col gap-10">
            <InfoHeader :content="qContent.data.value!" />
            <ChildrenList :content="qContent.data.value!" />
        </div>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import ASpinner from '@/ui/ASpinner.vue'
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

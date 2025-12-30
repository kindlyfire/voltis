<template>
    <VContainer>
        <div class="d-flex align-center gap-4 mb-4">
            <h1 class="text-h4">{{ library?.name }}</h1>
            <VBtn
                class="ms-1"
                :icon="true"
                variant="text"
                :size="'small'"
                :color="showStarredOnly ? 'yellow-darken-2' : undefined"
                title="Show starred only"
                @click="showStarredOnly = !showStarredOnly"
            >
                <VIcon>{{ showStarredOnly ? 'mdi-star' : 'mdi-star-outline' }}</VIcon>
            </VBtn>
        </div>
        <AContentGrid
            :items="qContents.data.value?.data ?? []"
            :loading="qContents.isLoading.value"
        />
    </VContainer>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { useHead } from '@unhead/vue'
import AContentGrid from '@/components/AContentGrid.vue'

const route = useRoute()
const libraryId = computed(() => route.params.id as string)
const showStarredOnly = ref(false)

const qLibraries = librariesApi.useList()
const library = computed(() => qLibraries.data?.value?.find(l => l.id === libraryId.value))

const qContents = contentApi.useList(
    computed(() => ({
        library_id: libraryId.value,
        parent_id: 'null',
        starred: showStarredOnly.value ? true : undefined,
    }))
)

useHead({
    title() {
        return library.value?.name ?? 'Library'
    },
})
</script>

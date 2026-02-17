<template>
    <VContainer>
        <div
            v-if="!qLibraries.isLoading.value && libraries?.length === 0 && user"
            class="d-flex flex-column align-center justify-center"
            style="min-height: 75vh"
        >
            <template v-if="user.permissions.includes('ADMIN')">
                <div class="text-h6 text-medium-emphasis mb-4">
                    No libraries. Add one in settings!
                </div>
                <VBtn color="primary" to="/settings/libraries">Libraries</VBtn>
            </template>
            <template v-else>
                <div class="text-h6 text-medium-emphasis mb-4">
                    No libraries. Ask your server admin to import something!
                </div>
            </template>
        </div>
        <div v-else class="space-y-8">
            <section v-if="lastRead?.length">
                <ACarousel title="Recently Read">
                    <template v-if="qLastRead.isLoading.value">
                        <ACarouselItem v-for="i in 3" :key="i">
                            <AContentGridItemSkeleton />
                        </ACarouselItem>
                    </template>
                    <template v-else>
                        <ACarouselItem v-for="item in lastRead" :key="item.id">
                            <AContentGridItem :content="item" />
                        </ACarouselItem>
                    </template>
                </ACarousel>
            </section>

            <section class="mt-4">
                <ACarousel title="Newly Added">
                    <template v-if="qNewest.isLoading.value">
                        <ACarouselItem v-for="i in 3" :key="i">
                            <AContentGridItemSkeleton />
                        </ACarouselItem>
                    </template>
                    <template v-else>
                        <ACarouselItem v-for="item in newest?.data ?? []" :key="item.id">
                            <AContentGridItem :content="item" />
                        </ACarouselItem>
                    </template>
                </ACarousel>
            </section>
        </div>
    </VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useHead } from '@unhead/vue'
import ACarousel from '@/components/ACarousel.vue'
import ACarouselItem from '@/components/ACarouselItem.vue'
import AContentGridItem from '@/components/AContentGridItem.vue'
import AContentGridItemSkeleton from '@/components/AContentGridItemSkeleton.vue'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { usersApi } from '@/utils/api/users'

useHead({
    title: 'Home',
})

const qLibraries = librariesApi.useList()
const libraries = computed(() => qLibraries.data.value)
const qUser = usersApi.useMe()
const user = qUser.data

const qLastRead = contentApi.useList({
    reading_status: 'reading',
    sort: 'progress_updated_at',
    sort_order: 'desc',
    type: ['book', 'comic'],
    limit: 10,
})
const lastRead = computed(() => qLastRead.data.value?.data ?? [])

const qNewest = contentApi.useList({
    parent_id: 'null',
    sort: 'created_at',
    sort_order: 'desc',
    limit: 10,
})
const newest = computed(() => qNewest.data.value)
</script>

<template>
    <div class="page-frame">
        <div
            v-if="!qLibraries.isLoading.value && libraries?.length === 0 && user"
            class="flex min-h-[70dvh] flex-col items-center justify-center gap-4 text-center"
        >
            <h1 class="font-display text-[30px] font-semibold">No libraries yet</h1>
            <template v-if="user.permissions.includes('ADMIN')">
                <p class="text-fg-muted">Add one in the settings to get started.</p>
                <AButton to="/settings/libraries">Libraries</AButton>
            </template>
            <p v-else class="text-fg-muted">Ask your server admin to import something.</p>
        </div>
        <div v-else class="flex flex-col gap-9">
            <h1 class="sr-only">Home</h1>
            <QueryError :query="qLastRead" />
            <AScrollRow
                v-if="qLastRead.isLoading.value || lastRead.length"
                title="Recently Read"
                :aria-busy="qLastRead.isLoading.value || undefined"
            >
                <template v-if="qLastRead.isLoading.value">
                    <ContentGridItemSkeleton v-for="i in 6" :key="i" subtitle />
                </template>
                <ContentGridItem
                    v-for="e in lastRead"
                    v-else
                    :key="e.item.id"
                    :content="e.item"
                    :series="e.series"
                    to-read-route
                />
            </AScrollRow>

            <QueryError :query="qNewest" />
            <AScrollRow
                v-if="qNewest.isLoading.value || newest.length"
                title="Newly Added"
                :aria-busy="qNewest.isLoading.value || undefined"
            >
                <template v-if="qNewest.isLoading.value">
                    <ContentGridItemSkeleton v-for="i in 6" :key="i" />
                </template>
                <ContentGridItem v-for="item in newest" v-else :key="item.id" :content="item" />
            </AScrollRow>
            <p v-else-if="qNewest.isSuccess.value" class="text-fg-muted py-12 text-center">
                Nothing has been added yet.
            </p>
        </div>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import ContentGridItem from '@/components/ContentGrid/Item.vue'
import ContentGridItemSkeleton from '@/components/ContentGrid/ItemSkeleton.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import AScrollRow from '@/ui/AScrollRow.vue'
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

const qLastRead = contentApi.useRecentlyRead(10)
const lastRead = computed(() => qLastRead.data.value ?? [])

const qNewest = contentApi.useList({
    parent_id: 'null',
    sort: 'created_at',
    sort_order: 'desc',
    limit: 10,
})
const newest = computed(() => qNewest.data.value?.data ?? [])
</script>

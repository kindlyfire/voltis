<template>
    <VListSubheader>
        <template v-if="qLibraries.isLoading.value || libraries?.length">Libraries</template>
        <template v-else>No libraries...</template>
    </VListSubheader>
    <VListItem
        v-for="library in shownLibraries"
        :key="library.id"
        :to="`/${library.id}`"
        prepend-icon="mdi-bookshelf"
        :active="library.id === activeLibraryId"
    >
        <VListItemTitle>{{ library.name }}</VListItemTitle>
    </VListItem>
    <VListItem
        v-if="activeNonShownLibrary"
        :key="activeNonShownLibrary.id"
        :to="`/${activeNonShownLibrary.id}`"
        prepend-icon="mdi-bookshelf"
        active
    >
        <VListItemTitle>{{ activeNonShownLibrary.name }}</VListItemTitle>
    </VListItem>
    <VMenu v-if="overflowLibraries.length" location="end">
        <template #activator="{ props }">
            <VListItem v-bind="props" prepend-icon="mdi-dots-horizontal">
                <VListItemTitle>Others</VListItemTitle>
            </VListItem>
        </template>
        <VList nav>
            <VListItem
                v-for="library in overflowLibraries"
                :key="library.id"
                :to="`/${library.id}`"
            >
                <VListItemTitle class="min-w-[100px]">{{ library.name }}</VListItemTitle>
            </VListItem>
        </VList>
    </VMenu>
</template>

<script setup lang="ts">
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { usersApi } from '@/utils/api/users'

const route = useRoute()
const qMe = usersApi.useMe()
const qLibraries = librariesApi.useList()
const libraries = qLibraries.data

const routeId = computed(() => route.params.id as string | undefined)
const qContent = contentApi.useGet(
    computed(() => (routeId.value?.startsWith('c_') ? routeId.value : null))
)

const libraryVisibility = computed(() => {
    return qMe.data.value?.preferences?.libraries ?? {}
})

const shownLibraries = computed(() => {
    return (qLibraries.data?.value ?? []).filter(l => {
        const v = libraryVisibility.value[l.id]?.visibility
        return !v || v === 'show'
    })
})

const overflowLibraries = computed(() => {
    return (qLibraries.data?.value ?? []).filter(
        l => libraryVisibility.value[l.id]?.visibility === 'overflow'
    )
})

const qActiveLibraryId = useQuery({
    queryKey: ['activeLibraryId', routeId],
    queryFn: async () => {
        const id = routeId.value
        if (id?.startsWith('l_')) return id
        if (id?.startsWith('c_')) {
            const content = await qContent.suspense()
            return content.data?.library_id ?? null
        }
        return null
    },
    placeholderData: keepPreviousData,
})
const activeLibraryId = qActiveLibraryId.data

const activeNonShownLibrary = computed(() => {
    const id = activeLibraryId.value
    if (!id) return null
    if (shownLibraries.value.some(l => l.id === id)) return null
    return (qLibraries.data?.value ?? []).find(l => l.id === id) ?? null
})
</script>

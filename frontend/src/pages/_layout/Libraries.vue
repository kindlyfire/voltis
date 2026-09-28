<template>
    <ANavLabel>
        {{ qLibraries.isLoading.value || libraries?.length ? 'Libraries' : 'No libraries' }}
    </ANavLabel>
    <ANavItem
        v-for="library in shownLibraries"
        :key="library.id"
        :to="`/${library.id}`"
        :icon="IconBookshelf"
        :active-icon="IconBookshelfFilled"
        :label="library.name"
        :active="library.id === activeLibraryId || undefined"
    />
    <ANavItem
        v-if="activeNonShownLibrary"
        :key="activeNonShownLibrary.id"
        :to="`/${activeNonShownLibrary.id}`"
        :icon="IconBookshelf"
        :active-icon="IconBookshelfFilled"
        :label="activeNonShownLibrary.name"
        active
    />
    <AMenu
        v-if="overflowLibraries.length"
        :side="store.sidebarTemporary.value ? 'bottom' : 'right'"
    >
        <template #trigger>
            <ANavItem :icon="IconDotsHorizontal" label="Others" />
        </template>
        <AMenuItem
            v-for="library in overflowLibraries"
            :key="library.id"
            :to="`/${library.id}`"
            :leading-icon="IconBookshelf"
        >
            {{ library.name }}
        </AMenuItem>
    </AMenu>
</template>

<script setup lang="ts">
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import ANavItem from '@/ui/ANavItem.vue'
import ANavLabel from '@/ui/ANavLabel.vue'
import { IconBookshelf, IconBookshelfFilled, IconDotsHorizontal } from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { usersApi } from '@/utils/api/users'
import { useLayoutStore } from './useLayoutStore'

const route = useRoute()
const store = useLayoutStore()
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

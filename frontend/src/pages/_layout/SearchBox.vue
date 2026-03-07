<template>
    <div class="search-box" ref="containerRef">
        <VMenu
            v-model="menuOpen"
            :close-on-content-click="false"
            offset="4"
            max-height="400"
            width="300"
        >
            <template #activator="{ props: menuProps }">
                <VTextField
                    v-bind="filteredMenuProps(menuProps)"
                    v-model="searchQuery"
                    placeholder="Search..."
                    prepend-inner-icon="mdi-magnify"
                    variant="outlined"
                    density="compact"
                    hide-details
                    single-line
                    clearable
                    @focus="focused = true"
                    @blur="focused = false"
                    @keydown.escape="close"
                />
            </template>
            <VList density="compact">
                <template v-if="query.data?.value?.data.length">
                    <VListItem
                        v-for="item in query.data.value.data"
                        :key="item.id"
                        @click="navigate(item.id)"
                    >
                        <template #prepend>
                            <img
                                v-if="item.cover_uri"
                                :src="`${API_URL}/files/cover/${item.id}?v=${item.file_mtime}`"
                                class="search-cover"
                            />
                            <div v-else class="search-cover bg-surface-variant" />
                        </template>
                        <VListItemTitle>{{ item.meta.title ?? item.title }}</VListItemTitle>
                        <VListItemSubtitle>{{ displayContentType(item.type) }}</VListItemSubtitle>
                    </VListItem>
                </template>
                <VListItem v-else-if="query.isFetching?.value">
                    <VListItemTitle class="text-center text-medium-emphasis">
                        Searching...
                    </VListItemTitle>
                </VListItem>
                <VListItem v-else>
                    <VListItemTitle class="text-center text-medium-emphasis">
                        No results
                    </VListItemTitle>
                </VListItem>
            </VList>
        </VMenu>
    </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { refDebounced, useMagicKeys, whenever } from '@vueuse/core'
import { useRouter } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
import { displayContentType } from '@/utils/misc'
import type { ContentListParams } from '@/utils/api/types'

const router = useRouter()
const searchQuery = ref('')
const focused = ref(false)
const menuOpen = ref(false)

const containerRef = ref<HTMLElement | null>(null)
const { ctrl_k } = useMagicKeys({
    passive: false,
    onEventFired: e => {
        if (e.ctrlKey && e.key === 'k') e.preventDefault()
    },
})
whenever(ctrl_k!, () => {
    containerRef.value?.querySelector('input')?.focus()
})

const debouncedQuery = refDebounced(searchQuery, 300)

watch([focused, debouncedQuery], () => {
    menuOpen.value = focused.value && !!debouncedQuery.value
})

const query = contentApi.useList(
    computed(() =>
        debouncedQuery.value
            ? <ContentListParams>{ search: debouncedQuery.value, limit: 10, parent_id: 'null' }
            : undefined
    )
)

function filteredMenuProps(props: Record<string, any>) {
    const { onClick, role, ...rest } = props
    return rest
}

function navigate(id: string) {
    close()
    router.push(`/${id}`)
}

function close() {
    menuOpen.value = false
    searchQuery.value = ''
}
</script>

<style scoped>
.search-box {
    width: 300px;
}

.search-cover {
    width: 40px;
    height: 60px;
    object-fit: cover;
    border-radius: 4px;
    margin-right: 12px;
}

.search-box :deep(input[aria-controls]) {
    cursor: text;
}
</style>

<template>
    <div class="page-frame">
        <ContentGrid
            title="All libraries"
            store-key="browse"
            :params="params"
            :extra-sorts="EXTRA_SORTS"
        />
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import ContentGrid from '@/components/ContentGrid/ContentGrid.vue'
import type { ContentListParams } from '@/utils/api/types'

useHead({ title: 'All libraries' })

const EXTRA_SORTS: { label: string; value: NonNullable<ContentListParams['sort']> }[] = [
    { label: 'Continue reading', value: 'continue' },
    { label: 'Recently updated', value: 'recently_updated' },
    { label: 'Reading history', value: 'history' },
]

const route = useRoute()
// The continue sorts list volumes wherever they sit; the others list the top level.
const params = computed<ContentListParams>(() =>
    route.query.sort === 'continue' || route.query.sort === 'recently_updated'
        ? {}
        : { parent_id: 'null' }
)
</script>

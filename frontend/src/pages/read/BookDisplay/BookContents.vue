<template>
    <nav v-if="usable" aria-label="Table of contents">
        <ul>
            <li v-for="entry in structure.toc" :key="entry.id">
                <ANavItem
                    :to="entry.href ? linkFor(entry.href, entry.fragment) : undefined"
                    :active="isActive(entry.id)"
                    :disabled="!entry.href"
                    :indent="entry.depth"
                    wrap
                    :class="{
                        'text-fg-muted': entry.href && entry.depth > 0 && !isActive(entry.id),
                    }"
                    @click="emit('select')"
                >
                    {{ entry.title || entry.href }}
                </ANavItem>
            </li>
        </ul>
    </nav>
    <nav v-else :aria-labelledby="headingId">
        <p class="text-fg-muted mb-2 text-sm">
            This book has no usable table of contents, so its files are listed instead.
        </p>
        <h3 :id="headingId" class="mb-1 text-sm font-medium">Book sections</h3>
        <ul>
            <li v-for="(item, index) in linearSpine" :key="item.href">
                <ANavItem
                    :to="linkFor(item.href, '')"
                    :active="activeHref === item.href"
                    wrap
                    @click="emit('select')"
                >
                    <template #prefix>{{ index + 1 }}</template>
                    {{ item.title || item.href }}
                </ANavItem>
            </li>
        </ul>
    </nav>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import ANavItem from '@/ui/ANavItem.vue'
import type { BookStructure } from '@/utils/api/types'
import { hasUsableToc } from './buildPages'

interface Props {
    contentId: string
    structure: BookStructure
    entryPages?: Record<string, number>
    activePage?: number | null
    activeHref?: string | null
    /** Forces the spine fallback even when the TOC looked usable, for when no
     * target actually resolved. Absent leaves the decision to the cheap check. */
    fallback?: boolean
}

// `undefined` keeps Vue from casting the absent boolean prop to false.
const props = withDefaults(defineProps<Props>(), { fallback: undefined })

const emit = defineEmits<{ select: [] }>()

const headingId = useId()

const usable = computed(() =>
    props.fallback == null ? hasUsableToc(props.structure) : !props.fallback
)
const linearSpine = computed(() => props.structure.spine.filter(item => item.linear))

function linkFor(href: string, fragment: string) {
    return {
        path: `/r/${props.contentId}`,
        query: fragment ? { ch: href, frag: fragment } : { ch: href },
    }
}

function isActive(entryId: string) {
    return props.activePage != null && props.entryPages?.[entryId] === props.activePage
}
</script>

<template>
    <div v-if="usable">
        <VList density="compact" nav>
            <VListItem
                v-for="entry in structure.toc"
                :key="entry.id"
                :to="entry.href ? linkFor(entry.href, entry.fragment) : undefined"
                :active="isActive(entry.id)"
                :disabled="!entry.href"
                :style="{ paddingInlineStart: `${entry.depth * 1.25 + 0.5}rem` }"
                @click="emit('select')"
            >
                <VListItemTitle :class="entry.depth === 0 ? 'font-medium' : 'opacity-80'">
                    {{ entry.title || entry.href }}
                </VListItemTitle>
            </VListItem>
        </VList>
    </div>
    <div v-else>
        <p class="mb-2 text-sm opacity-60">
            This book has no usable table of contents, so its files are listed instead.
        </p>
        <h3 class="mb-1 text-sm font-medium">Book sections</h3>
        <VList density="compact" nav>
            <VListItem
                v-for="(item, index) in linearSpine"
                :key="item.href"
                :to="linkFor(item.href, '')"
                :active="activeHref === item.href"
                @click="emit('select')"
            >
                <template #prepend>
                    <span class="mr-4 opacity-60">{{ index + 1 }}</span>
                </template>
                <VListItemTitle>{{ item.title || item.href }}</VListItemTitle>
            </VListItem>
        </VList>
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
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

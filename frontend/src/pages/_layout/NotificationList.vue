<template>
    <ul v-if="shown.length" class="-mx-2.5 flex flex-col">
        <NotificationItem
            v-for="item in shown"
            :key="item.id"
            :item="item"
            :full="full"
            @navigate="emit('navigate')"
        />
    </ul>
    <p v-else-if="store.loaded" class="text-fg-muted py-4 text-center text-sm">Nothing here yet.</p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useNotificationStore } from '@/stores/notifications'
import NotificationItem from './NotificationItem.vue'

const props = defineProps<{
    full?: boolean
    /** Caps the whole list; the running and needs-input items always show. */
    historyLimit?: number
}>()
const emit = defineEmits<{ navigate: [] }>()

const store = useNotificationStore()

const shown = computed(() => {
    const head = [...store.active, ...store.needsInput]
    // The limit is on the whole list; the running and needs-input items always show.
    const room =
        props.historyLimit === undefined ? undefined : Math.max(0, props.historyLimit - head.length)
    return [...head, ...store.history.slice(0, room)]
})
</script>

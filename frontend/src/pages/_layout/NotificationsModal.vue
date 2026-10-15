<template>
    <ADialog :open="open" title="Notifications" size="lg" @update:open="v => !v && close()">
        <NotificationList full @navigate="close()" />
        <div v-if="store.next" class="mt-2 flex justify-center">
            <AButton variant="text" size="sm" :loading="loadingMore" @click="more">
                Show more
            </AButton>
        </div>

        <template #actions>
            <ScanAllButton class="mr-auto" />
            <AButton
                variant="text"
                tone="neutral"
                :disabled="!store.items.length"
                @click="store.clear()"
            >
                Clear all
            </AButton>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useNotificationStore } from '@/stores/notifications'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import NotificationList from './NotificationList.vue'
import ScanAllButton from './ScanAllButton.vue'

defineProps<{ open: boolean; close: () => void }>()

const store = useNotificationStore()
const loadingMore = ref(false)

// loadMore marks each further page it loads.
void store.markSeen(store.items)

async function more() {
    loadingMore.value = true
    await store.loadMore()
    loadingMore.value = false
}
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './NotificationsModal.vue'

export function showNotificationsModal(options?: ShowOptions): Promise<void> {
    return Modals.show<void>(Self, {}, options)
}
</script>

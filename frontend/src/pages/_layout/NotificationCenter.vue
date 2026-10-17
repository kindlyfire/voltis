<template>
    <APopover v-model:open="open" label="Notifications" align="end" :width="360">
        <template #trigger>
            <AIconButton
                :icon="IconNotifications"
                :label="unread ? `Notifications, ${unread} unread` : 'Notifications'"
                :badge="unread ? (unread > 9 ? '9+' : String(unread)) : undefined"
                :class="{ ring: scanning }"
                data-notifications-trigger
            />
        </template>
        <!-- Only the list scrolls; the padding keeps the rows' hover background and focus rings unclipped. -->
        <div class="-mx-2.5 max-h-[min(28rem,60vh)] overflow-y-auto overscroll-contain px-2.5">
            <ScrobbleQueue @navigate="open = false" />
            <NotificationList :history-limit="10" @navigate="open = false" />
        </div>
        <div class="border-outline-variant mt-1 -mb-1 flex justify-between border-t pt-1">
            <ScanAllButton />
            <AButton variant="text" size="sm" class="ml-auto" @click="showAll">Show all</AButton>
        </div>
    </APopover>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useNotificationStore } from '@/stores/notifications'
import { isTerminal, useScanStore } from '@/stores/scans'
import AButton from '@/ui/AButton.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APopover from '@/ui/APopover.vue'
import { IconNotifications } from '@/ui/icons'
import NotificationList from './NotificationList.vue'
import { showNotificationsModal } from './NotificationsModal.vue'
import ScanAllButton from './ScanAllButton.vue'
import ScrobbleQueue from './ScrobbleQueue.vue'

const store = useNotificationStore()
const scanStore = useScanStore()
const open = ref(false)

const unread = computed(() => store.badge)
// From the live scans, not the feed: a dismissed running scan keeps the bell ringing.
const scanning = computed(() =>
    Object.values(scanStore.tasks).some(task => task.name === 'scan_library' && !isTerminal(task))
)

// Opening shows everything loaded, which is more than the rows on screen.
watch(open, isOpen => {
    if (isOpen) void store.markSeen(store.items)
})

// "Show all" unmounts with the popover, so focus returns to the popover's trigger. It is found in
// the DOM because AIconButton's root is a fragment.
function showAll() {
    open.value = false
    void showNotificationsModal({
        focusFallback: () => document.querySelector<HTMLElement>('[data-notifications-trigger]'),
    })
}
</script>

<style scoped>
.ring :deep(svg) {
    transform-origin: 50% 0;
    animation: ring 1.6s ease-in-out infinite;
}

@media (prefers-reduced-motion: reduce) {
    .ring :deep(svg) {
        animation: none;
    }
}

@keyframes ring {
    0%,
    50%,
    100% {
        transform: rotate(0);
    }
    10%,
    30% {
        transform: rotate(12deg);
    }
    20%,
    40% {
        transform: rotate(-12deg);
    }
}
</style>

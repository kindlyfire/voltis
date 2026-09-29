<template>
    <ADialog :open="open && !stale" :title="appKey.name" @update:open="v => !v && close()">
        <div v-if="!stale" class="flex flex-col gap-4">
            <ATextField
                v-for="f in fields"
                :key="f.label"
                :model-value="f.url"
                :label="f.label"
                readonly
            >
                <template #trailing>
                    <AIconButton
                        :icon="IconContentCopy"
                        :label="`Copy the ${f.label} URL`"
                        size="sm"
                        @click="copy(f)"
                    />
                </template>
            </ATextField>
            <p class="text-fg-muted text-sm">Anyone with these links can read your library.</p>
        </div>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconContentCopy } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { usersApi } from '@/utils/api/users'

const props = defineProps<{
    open: boolean
    close: () => void
    appKey: AppKey
    userId: string
}>()

const toast = useToast()
const me = usersApi.useMe()

// Another user signing in (another tab, forwarded auth) must not see this user's key.
const stale = computed(() => me.data.value?.id !== props.userId)
watch(stale, s => s && props.close(), { immediate: true })

const fields = [
    { label: 'OPDS 1.2 feed', url: props.appKey.feeds.v1 },
    { label: 'OPDS 2.0 feed', url: props.appKey.feeds.v2 },
]

async function copy(f: (typeof fields)[number]) {
    try {
        await navigator.clipboard.writeText(f.url)
        toast.show({ message: `Copied the ${f.label} URL`, tone: 'info' })
    } catch {
        toast.show({ message: 'Could not copy the URL', tone: 'danger' })
    }
}
</script>

<script lang="ts">
import type { AppKey } from '@/utils/api/types'
import { Modals } from '@/utils/modals'
import Self from './OpdsKeyModal.vue'

export function showOpdsKeyModal(appKey: AppKey, userId: string): Promise<void> {
    return Modals.show<void>(Self, { appKey, userId })
}
</script>

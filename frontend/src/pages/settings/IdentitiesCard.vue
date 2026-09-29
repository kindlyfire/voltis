<template>
    <ACard
        ref="card"
        title="Linked accounts"
        :variant="compact ? 'tonal' : 'raised'"
        :padding="compact ? 'md' : 'lg'"
        :heading-level="compact ? 3 : 2"
        tabindex="-1"
    >
        <p v-if="!identities.length" class="text-fg-muted text-sm">
            No external account is linked.
        </p>
        <ATable v-else density="compact" class="-mx-4 max-w-none">
            <thead>
                <tr>
                    <th>Provider</th>
                    <th>Account</th>
                    <th>Linked</th>
                    <th><span class="sr-only">Actions</span></th>
                </tr>
            </thead>
            <tbody>
                <tr v-for="(identity, index) in identities" :key="identity.id">
                    <td>{{ identity.provider === 'oidc' ? 'SSO' : 'Proxy' }}</td>
                    <td class="py-1.5">
                        <div class="break-words">{{ accountLabel(identity) }}</div>
                        <div
                            v-if="accountDetail(identity)"
                            class="text-fg-muted text-xs [overflow-wrap:anywhere]"
                        >
                            {{ accountDetail(identity) }}
                        </div>
                    </td>
                    <td class="whitespace-nowrap">
                        {{ new Date(identity.created_at).toLocaleDateString() }}
                    </td>
                    <td class="text-end">
                        <AIconButton
                            :icon="IconLinkOff"
                            :label="`Unlink ${accountLabel(identity)}`"
                            size="sm"
                            :loading="
                                unlink.isPending.value && unlink.variables.value === identity.id
                            "
                            data-unlink
                            @click="handleUnlink(identity, index)"
                        />
                    </td>
                </tr>
            </tbody>
        </ATable>

        <QueryError :mutation="unlink" />
        <slot />
    </ACard>
</template>

<script setup lang="ts">
import { computed, nextTick, useTemplateRef, watch } from 'vue'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import ACard from '@/ui/ACard.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ATable from '@/ui/ATable.vue'
import { IconLinkOff } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import type { Identity } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'

const props = defineProps<{
    userId: string
    self?: boolean
    /** Inside a dialog: less padding, and an h3. */
    compact?: boolean
}>()

const emit = defineEmits<{ unlinked: [] }>()

const qIdentities = usersApi.useIdentities(() => props.userId)
const unlink = usersApi.useUnlinkIdentity(() => (props.self ? 'me' : props.userId))

const identities = computed(() => qIdentities.data.value ?? [])
const toast = useToast()
const card = useTemplateRef<{ $el: HTMLElement }>('card')

// OIDC subjects can be opaque (Dex), so the email names the account when there is one.
function accountLabel(identity: Identity) {
    return identity.provider === 'proxy' ? identity.subject : identity.email || identity.subject
}

function accountDetail(identity: Identity) {
    const subject = identity.subject === accountLabel(identity) ? '' : identity.subject
    return [subject, identity.issuer].filter(Boolean).join(' · ')
}

// An unlinked row's button disappears: focus the next row's, else the card (a dialog around this
// card would otherwise lose focus). The row goes either before or after the confirm dialog's
// focus return, so both paths use this.
let refocusIndex: number | null = null
function refocusTarget(index: number) {
    const buttons = card.value?.$el.querySelectorAll<HTMLElement>('[data-unlink]') ?? []
    return buttons[Math.min(index, buttons.length - 1)] ?? card.value?.$el
}
watch(identities, async () => {
    if (refocusIndex == null) return
    const index = refocusIndex
    refocusIndex = null
    await nextTick()
    refocusTarget(index)?.focus()
})

async function handleUnlink(identity: Identity, index: number) {
    const confirmed = await showConfirmModal(
        {
            title: 'Unlink account',
            message: props.self
                ? `Unlink ${accountLabel(identity)}? This signs you out of every device.`
                : `Unlink ${accountLabel(identity)} from this user? Their sessions end immediately.`,
            confirmText: 'Unlink',
            tone: 'danger',
        },
        { focusFallback: () => refocusTarget(index) }
    )
    if (!confirmed) return
    await unlink.mutateAsync(identity.id)
    refocusIndex = index
    toast.show({ message: `Unlinked ${accountLabel(identity)}` })
    emit('unlinked')
}
</script>

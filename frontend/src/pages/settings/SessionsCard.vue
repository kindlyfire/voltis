<template>
    <ACard ref="card" title="Sessions" tabindex="-1">
        <ATable v-if="sessions.length" density="compact" class="-mx-4 max-w-none">
            <thead>
                <tr>
                    <th>Device</th>
                    <th>Sign-in</th>
                    <th>Last used</th>
                    <th><span class="sr-only">Actions</span></th>
                </tr>
            </thead>
            <tbody>
                <tr v-for="session in sessions" :key="session.id">
                    <td class="py-1.5">
                        <div class="break-words">{{ deviceLabel(session) }}</div>
                        <div class="text-fg-muted text-xs">
                            Signed in {{ formatDate(session.created_at) }}
                        </div>
                    </td>
                    <td>{{ METHOD_LABELS[session.method] }}</td>
                    <td class="whitespace-nowrap">
                        {{ session.last_used_at ? formatDate(session.last_used_at) : '—' }}
                    </td>
                    <td class="text-end">
                        <span v-if="session.current" class="text-fg-muted text-xs">
                            This session
                        </span>
                        <AIconButton
                            v-else
                            :icon="IconLogout"
                            :label="`Sign out ${fullLabel(session)}`"
                            size="sm"
                            :loading="
                                revoke.isPending.value && revoke.variables.value === session.id
                            "
                            data-revoke
                            @click="handleRevoke(session)"
                        />
                    </td>
                </tr>
            </tbody>
        </ATable>

        <QueryError :mutation="revoke" />
    </ACard>
</template>

<script setup lang="ts">
import { computed, nextTick, useTemplateRef } from 'vue'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import ACard from '@/ui/ACard.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ATable from '@/ui/ATable.vue'
import { IconLogout } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import type { Session, SessionMethod } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'

const METHOD_LABELS: Record<SessionMethod, string> = {
    password: 'Password',
    oidc: 'SSO',
    proxy: 'Proxy',
}

const qSessions = usersApi.useSessions()
const revoke = usersApi.useRevokeSession()

const sessions = computed(() => qSessions.data.value ?? [])
const toast = useToast()
const card = useTemplateRef<{ $el: HTMLElement }>('card')

function deviceLabel(session: Session) {
    return session.client_name ?? 'Browser'
}

/** With the sign-in date, which tells browser rows apart. */
function fullLabel(session: Session) {
    return `${deviceLabel(session)}, signed in ${formatDate(session.created_at)}`
}

function formatDate(value: string) {
    return new Date(value).toLocaleDateString()
}

// A revoked row's button disappears: focus the next row's, else the previous one's, else the card.
function refocusTarget(index: number) {
    const buttons = card.value?.$el.querySelectorAll<HTMLElement>('[data-revoke]') ?? []
    return buttons[Math.min(index, buttons.length - 1)] ?? card.value?.$el
}

async function handleRevoke(session: Session) {
    const index = sessions.value.filter(s => !s.current).indexOf(session)
    const label = deviceLabel(session)

    const confirmed = await showConfirmModal(
        {
            title: 'Sign out session',
            message: `Sign out ${fullLabel(session)}?`,
            confirmText: 'Sign out',
            tone: 'danger',
        },
        { focusFallback: () => refocusTarget(index) }
    )
    if (!confirmed) return
    try {
        await revoke.mutateAsync(session.id)
    } catch {
        return // Shown by QueryError.
    }
    toast.show({ message: `Signed out ${label}` })
    await nextTick()
    refocusTarget(index)?.focus()
}
</script>

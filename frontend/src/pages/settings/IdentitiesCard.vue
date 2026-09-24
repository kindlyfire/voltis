<template>
    <VCard :flat="flat">
        <VCardTitle>Linked accounts</VCardTitle>
        <VCardText>
            <p v-if="!identities.length" class="mb-4 opacity-60">No external account is linked.</p>
            <VTable v-else density="compact" class="mb-4">
                <thead>
                    <tr>
                        <th>Provider</th>
                        <th>Account</th>
                        <th>Linked</th>
                        <th></th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="identity in identities" :key="identity.id">
                        <td>{{ identity.provider === 'oidc' ? 'SSO' : 'Proxy' }}</td>
                        <td>
                            <div>{{ identity.subject }}</div>
                            <div v-if="identity.issuer" class="text-xs opacity-60">
                                {{ identity.issuer }}
                            </div>
                        </td>
                        <td>{{ new Date(identity.created_at).toLocaleDateString() }}</td>
                        <td class="text-right">
                            <VBtn
                                icon="mdi-link-off"
                                variant="text"
                                size="small"
                                :title="unlinkTitle"
                                :loading="unlink.isPending.value"
                                @click="handleUnlink(identity)"
                            />
                        </td>
                    </tr>
                </tbody>
            </VTable>

            <AQueryError :mutation="unlink" />
            <slot />
        </VCardText>
    </VCard>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { showConfirmModal } from '@/components/AConfirmModal.vue'
import AQueryError from '@/components/AQueryError.vue'
import type { Identity } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'

const props = defineProps<{
    userId: string
    self?: boolean
    flat?: boolean
}>()

const emit = defineEmits<{ unlinked: [] }>()

const qIdentities = usersApi.useIdentities(() => props.userId)
const unlink = usersApi.useUnlinkIdentity(() => (props.self ? 'me' : props.userId))

const identities = computed(() => qIdentities.data.value ?? [])
const unlinkTitle = computed(() =>
    props.self ? 'Unlink (signs you out everywhere)' : 'Unlink from this user'
)

async function handleUnlink(identity: Identity) {
    const confirmed = await showConfirmModal({
        title: 'Unlink account',
        message: props.self
            ? `Unlink ${identity.subject}? This signs you out of every device.`
            : `Unlink ${identity.subject} from this user? Their sessions end immediately.`,
        confirmText: 'Unlink',
        confirmColor: 'error',
    })
    if (!confirmed) return
    await unlink.mutateAsync(identity.id)
    emit('unlinked')
}
</script>

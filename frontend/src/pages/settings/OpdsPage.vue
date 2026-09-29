<template>
    <div class="settings-page max-w-[780px]">
        <APageHeader title="OPDS" class="mb-1.5" />

        <ACard ref="card" title="Keys" tabindex="-1">
            <p v-if="!keys.length" class="text-fg-muted text-sm">No keys yet.</p>
            <ATable v-else density="compact" class="-mx-4 max-w-none">
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Created</th>
                        <th>Last used</th>
                        <th><span class="sr-only">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="(k, index) in keys" :key="k.id">
                        <td class="break-words">{{ k.name }}</td>
                        <td class="whitespace-nowrap">{{ date(k.created_at) }}</td>
                        <td class="whitespace-nowrap">
                            {{ k.last_used_at ? date(k.last_used_at) : 'Never' }}
                        </td>
                        <td class="text-end whitespace-nowrap">
                            <AIconButton
                                :icon="IconLink"
                                :label="`Show feed URLs for ${k.name}`"
                                size="sm"
                                @click="me.data.value && showOpdsKeyModal(k, me.data.value.id)"
                            />
                            <AIconButton
                                :icon="IconDelete"
                                :label="`Revoke ${k.name}`"
                                size="sm"
                                :loading="revoke.isPending.value && revoke.variables.value === k.id"
                                data-revoke
                                @click="handleRevoke(k, index)"
                            />
                        </td>
                    </tr>
                </tbody>
            </ATable>

            <QueryError :query="qKeys" />
            <QueryError :mutation="revoke" />
        </ACard>

        <ACard title="New key">
            <form :id="formId" novalidate @submit="form.onSubmit">
                <ATextField v-bind="form.field('name')" label="Name" />
                <QueryError :mutation="form.mutation" class="mt-3" />
            </form>
            <template #actions>
                <AButton type="submit" :form="formId" :loading="form.mutation.isPending.value">
                    Create
                </AButton>
            </template>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, nextTick, onUnmounted, useId, useTemplateRef, watch } from 'vue'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ATable from '@/ui/ATable.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconDelete, IconLink } from '@/ui/icons'
import { appKeysApi } from '@/utils/api/app-keys'
import type { AppKey } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'
import { showOpdsKeyModal } from './OpdsKeyModal.vue'

useHead({
    title: 'OPDS',
})

const me = usersApi.useMe()
const qKeys = appKeysApi.useKeys()
const create = appKeysApi.useCreateKey()
const revoke = appKeysApi.useRevokeKey()
const keys = computed(() => qKeys.data.value ?? [])
const card = useTemplateRef<{ $el: HTMLElement }>('card')
const formId = useId()
let disposed = false
onUnmounted(() => {
    disposed = true
})

const date = (iso: string) => new Date(iso).toLocaleDateString()

const form = useForm({
    schema: z.object({
        name: z.string().trim().min(1, 'Enter a name').max(100, 'Use at most 100 characters'),
    }),
    initialValues: {
        name: '',
    },
    onSubmit: async values => {
        const userId = me.data.value?.id
        const k = await create.mutateAsync(values.name)
        create.reset()
        // A create that finishes after leaving the page or signing out must not show the key.
        if (disposed || !userId || me.data.value?.id !== userId) return
        form.reset()
        void showOpdsKeyModal(k, userId)
    },
})

// A revoked row's button disappears: focus the next row's, else the card.
let refocusIndex: number | null = null
function refocusTarget(index: number) {
    const buttons = card.value?.$el.querySelectorAll<HTMLElement>('[data-revoke]') ?? []
    return buttons[Math.min(index, buttons.length - 1)] ?? card.value?.$el
}
watch(keys, async () => {
    if (refocusIndex == null) return
    const index = refocusIndex
    refocusIndex = null
    await nextTick()
    refocusTarget(index)?.focus()
})

async function handleRevoke(k: AppKey, index: number) {
    const confirmed = await showConfirmModal(
        {
            title: 'Revoke key',
            message: `Revoke ${k.name}? Apps using this key lose access.`,
            confirmText: 'Revoke',
            tone: 'danger',
        },
        { focusFallback: () => refocusTarget(index) }
    )
    if (!confirmed) return
    refocusIndex = index
    revoke.mutate(k.id, {
        onError: () => {
            refocusIndex = null
        },
    })
}
</script>

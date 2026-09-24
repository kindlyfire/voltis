<template>
    <div class="settings-page max-w-[780px]">
        <div class="mb-1.5 flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
            <APageHeader title="Users" />
            <AButton ref="createButton" :leading-icon="IconPlus" @click="showUserModal('new')">
                Create user
            </AButton>
        </div>

        <QueryError :query="users" />

        <ACard padding="none">
            <ATable>
                <thead>
                    <tr>
                        <th>Username</th>
                        <th>Created</th>
                        <th><span class="sr-only">Actions</span></th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-if="users.isLoading.value">
                        <td colspan="3"><ASpinner class="mx-auto flex" /></td>
                    </tr>
                    <tr v-for="user in users.data.value" :key="user.id">
                        <td>
                            <span class="flex flex-wrap items-center gap-2">
                                {{ user.username }}
                                <AChip v-if="user.permissions.includes('ADMIN')" size="sm">
                                    Admin
                                </AChip>
                            </span>
                        </td>
                        <td>{{ new Date(user.created_at).toLocaleDateString() }}</td>
                        <td class="text-end">
                            <AIconButton
                                :icon="IconPencil"
                                :label="`Edit ${user.username}`"
                                size="sm"
                                @click="showUserModal(user.id, { focusFallback })"
                            />
                        </td>
                    </tr>
                </tbody>
            </ATable>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { useTemplateRef } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AChip from '@/ui/AChip.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import { IconPencil, IconPlus } from '@/ui/icons'
import { usersApi } from '@/utils/api/users'
import { showUserModal } from './UserModal.vue'

useHead({
    title: 'Users',
})

const users = usersApi.useList()
const createButton = useTemplateRef<{ $el: HTMLElement }>('createButton')
// A deleted user's Edit button is gone.
const focusFallback = () => createButton.value?.$el
</script>

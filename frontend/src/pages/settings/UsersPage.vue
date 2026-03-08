<template>
    <VContainer>
        <div class="d-flex align-center mb-6">
            <h1 class="text-h4">Users</h1>
            <VSpacer />
            <VBtn color="primary" @click="showUserModal('new')">Create User</VBtn>
        </div>

        <VTable>
            <thead>
                <tr>
                    <th>Username</th>
                    <th>Created</th>
                    <th></th>
                </tr>
            </thead>
            <tbody>
                <tr v-for="user in users.data?.value" :key="user.id">
                    <td>{{ user.username }}</td>
                    <td>{{ new Date(user.created_at).toLocaleDateString() }}</td>
                    <td>
                        <VBtn
                            icon="mdi-pencil"
                            variant="text"
                            size="small"
                            @click="showUserModal(user.id)"
                        />
                    </td>
                </tr>
            </tbody>
        </VTable>
    </VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { usersApi } from '@/utils/api/users'
import { showUserModal } from './UserModal.vue'

useHead({
    title: 'Users',
})

const users = usersApi.useList()
</script>

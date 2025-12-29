<template>
    <VContainer>
        <div class="d-flex align-center mb-6">
            <h1 class="text-h4">Users</h1>
            <VSpacer />
            <VBtn color="primary" @click="((selectedUserId = 'new'), (userModalOpen = true))"
                >Create User</VBtn
            >
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
                            @click="((selectedUserId = user.id), (userModalOpen = true))"
                        />
                    </td>
                </tr>
            </tbody>
        </VTable>

        <UserModal :user-id="selectedUserId" v-model="userModalOpen" />
    </VContainer>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { usersApi } from '@/utils/api/users'
import UserModal from './UserModal.vue'
import { useHead } from '@unhead/vue'

useHead({
    title: 'Users',
})

const users = usersApi.useList()
const userModalOpen = ref(false)
const selectedUserId = ref<string | null>(null)
</script>

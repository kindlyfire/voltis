<template>
    <VContainer>
        <h1 class="text-h4 mb-6">Interface</h1>

        <VCard>
            <VCardTitle>Library Visibility</VCardTitle>
            <VCardText>
                <div v-if="!qLibraries.data?.value?.length" class="text-medium-emphasis">
                    No libraries found.
                </div>
                <div v-else class="d-flex flex-column gap-4">
                    <div
                        v-for="library in qLibraries.data.value"
                        :key="library.id"
                        class="d-flex align-center gap-4"
                    >
                        <span class="text-body-1" style="min-width: 120px">
                            {{ library.name }}
                        </span>
                        <VBtnToggle
                            :model-value="getVisibility(library.id)"
                            @update:model-value="v => setVisibility(library.id, v)"
                            mandatory
                            density="compact"
                            divided
                            variant="outlined"
                        >
                            <VBtn value="show">Show</VBtn>
                            <VBtn value="overflow">Overflow</VBtn>
                            <VBtn value="hide">Hide</VBtn>
                        </VBtnToggle>
                    </div>
                </div>
                <AQueryError :mutation="mutation" />
                <VBtn
                    color="primary"
                    class="mt-6"
                    :loading="mutation.isPending.value"
                    @click="save"
                >
                    Save
                </VBtn>
            </VCardText>
        </VCard>
    </VContainer>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { usersApi } from '@/utils/api/users'
import { librariesApi } from '@/utils/api/libraries'
import type { LibraryPreference } from '@/utils/api/types'
import AQueryError from '@/components/AQueryError.vue'
import { useHead } from '@unhead/vue'
import { jsonClone } from '@/utils/misc'

useHead({ title: 'Interface' })

const qMe = usersApi.useMe()
const qLibraries = librariesApi.useList()
const mutation = usersApi.useUpdateMe()

const libraryPrefs = ref<Record<string, LibraryPreference>>({})

watch(
    () => qMe.data.value,
    user => {
        if (user) {
            libraryPrefs.value = jsonClone(user.preferences.libraries ?? {})
        }
    },
    { immediate: true }
)

function getVisibility(libraryId: string): string {
    return libraryPrefs.value[libraryId]?.visibility ?? 'show'
}

function setVisibility(libraryId: string, value: string) {
    if (value === 'show') {
        if (libraryPrefs.value[libraryId]) {
            delete libraryPrefs.value[libraryId].visibility
            if (Object.keys(libraryPrefs.value[libraryId]).length === 0) {
                delete libraryPrefs.value[libraryId]
            }
        }
    } else {
        if (!libraryPrefs.value[libraryId]) {
            libraryPrefs.value[libraryId] = {}
        }
        libraryPrefs.value[libraryId].visibility = value as LibraryPreference['visibility']
    }
}

async function save() {
    const me = qMe.data.value
    if (!me) return
    await mutation.mutateAsync({
        username: me.username,
        preferences: { libraries: libraryPrefs.value },
    })
}
</script>

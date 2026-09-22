<template>
    <VContainer>
        <h1 class="mb-6 text-4xl">Interface</h1>

        <VCard>
            <VCardTitle>Library Visibility</VCardTitle>
            <VCardText>
                <div v-if="!qLibraries.data?.value?.length" class="opacity-60">
                    No libraries found.
                </div>
                <div v-else class="flex flex-col gap-4">
                    <div
                        v-for="library in qLibraries.data.value"
                        :key="library.id"
                        class="flex flex-wrap items-center gap-4"
                    >
                        <span class="text-base" style="min-width: 120px">
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

        <VCard class="mt-6">
            <VCardTitle>Reader</VCardTitle>
            <VCardText>
                <div class="flex flex-wrap gap-4">
                    <VBtn variant="tonal" @click="showReaderTutorial('comic')">
                        Show comic tutorial
                    </VBtn>
                    <VBtn variant="tonal" @click="showReaderTutorial('book')">
                        Show book tutorial
                    </VBtn>
                </div>
            </VCardText>
        </VCard>
    </VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { ref, watch } from 'vue'
import AQueryError from '@/components/AQueryError.vue'
import { showReaderTutorial } from '@/pages/read/ReaderTutorialModal.vue'
import { librariesApi } from '@/utils/api/libraries'
import type { LibraryPreference, PreferencesPatch } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { jsonClone } from '@/utils/misc'

useHead({ title: 'Interface' })

const qMe = usersApi.useMe()
const qLibraries = librariesApi.useList()
const mutation = usersApi.usePatchPreferences()

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

    // A merge patch only carries what is sent, so removals need an explicit
    // null. Stored preferences are arbitrary JSON: an entry that keeps other
    // keys would otherwise hold on to its old `visibility`.
    const server = me.preferences.libraries ?? {}
    const local = libraryPrefs.value
    const libraries: NonNullable<PreferencesPatch['libraries']> = {}
    for (const id of new Set([...Object.keys(server), ...Object.keys(local)])) {
        const entry = local[id]
        libraries[id] = entry ? { visibility: entry.visibility ?? null } : null
    }

    await mutation.mutateAsync({ libraries })
}
</script>

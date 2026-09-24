<template>
    <div class="settings-page max-w-[780px]">
        <APageHeader title="Interface" class="mb-1.5" />

        <ACard title="Library visibility">
            <QueryError :query="qLibraries" />
            <div v-if="qLibraries.isLoading.value" class="flex justify-center py-6">
                <ASpinner />
            </div>
            <p v-else-if="qLibraries.isSuccess.value && !libraries.length" class="text-fg-muted">
                No libraries yet.
            </p>
            <div
                v-for="library in libraries"
                :key="library.id"
                class="flex min-h-14 items-center justify-between gap-x-4 gap-y-2 max-sm:flex-col max-sm:items-start max-sm:py-1"
            >
                <span class="flex min-w-0 items-center gap-3 text-[15px]">
                    <AIcon :icon="IconBookshelf" :size="22" class="text-fg-muted" />
                    {{ library.name }}
                </span>
                <ASegmented
                    :model-value="getVisibility(library.id)"
                    :options="visibilityOptions"
                    :label="`${library.name} visibility`"
                    @update:model-value="v => setVisibility(library.id, v)"
                />
            </div>
            <QueryError :mutation="mutation" />
            <template #actions>
                <AButton :loading="mutation.isPending.value" @click="save">Save</AButton>
            </template>
        </ACard>

        <ACard title="Reader">
            <div class="flex flex-wrap gap-3">
                <AButton
                    variant="tonal"
                    :leading-icon="IconAutoStories"
                    @click="showReaderTutorial('comic')"
                >
                    Show comic tutorial
                </AButton>
                <AButton
                    variant="tonal"
                    :leading-icon="IconBookOpen"
                    @click="showReaderTutorial('book')"
                >
                    Show book tutorial
                </AButton>
            </div>
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, ref, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import { showReaderTutorial } from '@/pages/read/ReaderTutorialModal.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AIcon from '@/ui/AIcon.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconAutoStories, IconBookOpen, IconBookshelf } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { librariesApi } from '@/utils/api/libraries'
import type { LibraryPreference, PreferencesPatch } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { jsonClone } from '@/utils/misc'

useHead({ title: 'Interface' })

const qMe = usersApi.useMe()
const qLibraries = librariesApi.useList()
const mutation = usersApi.usePatchPreferences()
const libraries = computed(() => qLibraries.data.value ?? [])
const toast = useToast()

type Visibility = NonNullable<LibraryPreference['visibility']>
const visibilityOptions = [
    { value: 'show', label: 'Show' },
    { value: 'overflow', label: 'Overflow' },
    { value: 'hide', label: 'Hide' },
] as const

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

function getVisibility(libraryId: string): Visibility {
    return libraryPrefs.value[libraryId]?.visibility ?? 'show'
}

function setVisibility(libraryId: string, value: Visibility) {
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
        libraryPrefs.value[libraryId].visibility = value
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
    toast.show({ message: 'Saved library visibility' })
}
</script>

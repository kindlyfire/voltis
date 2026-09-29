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

        <ACard title="Home">
            <ASwitch
                v-model="ignoreSeriesStatus"
                label="Show on-hold and dropped series in Recently Read"
                description="Otherwise a series you put on hold or dropped is hidden, even while you read one of its items."
                :readonly="homeMutation.isPending.value"
                @update:model-value="saveHome"
            />
            <QueryError :mutation="homeMutation" />
        </ACard>

        <ACard title="Reading speed">
            <form
                :id="speedFormId"
                novalidate
                class="flex flex-col gap-3"
                @submit="speedForm.onSubmit"
            >
                <ATextField
                    v-bind="speedForm.field('wordsPerMinute')"
                    label="Books"
                    type="number"
                    inputmode="numeric"
                    min="50"
                    max="2000"
                    suffix="words/min"
                />
                <ATextField
                    v-bind="speedForm.field('secondsPerPage')"
                    label="Comics"
                    type="number"
                    inputmode="numeric"
                    min="1"
                    max="600"
                    suffix="s/page"
                />
                <QueryError :mutation="speedForm.mutation" />
            </form>
            <template #actions>
                <AButton
                    type="submit"
                    :form="speedFormId"
                    :loading="speedForm.mutation.isPending.value"
                >
                    Save
                </AButton>
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
import { computed, ref, useId, watch } from 'vue'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import { showReaderTutorial } from '@/pages/read/ReaderTutorialModal.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AIcon from '@/ui/AIcon.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconAutoStories, IconBookOpen, IconBookshelf } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { invalidateRecentlyRead } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import type { LibraryPreference, PreferencesPatch } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { useForm } from '@/utils/forms'
import { jsonClone } from '@/utils/misc'
import {
    DEFAULT_SECONDS_PER_PAGE,
    DEFAULT_WORDS_PER_MINUTE,
    readingSpeed,
} from '@/utils/readingTime'

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

// Only `libraries`: other saves replace the user, which would wipe unsaved edits.
watch(
    () => qMe.data.value?.preferences.libraries,
    libraries => {
        libraryPrefs.value = jsonClone(libraries ?? {})
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

const homeMutation = usersApi.usePatchPreferences()
// Local, since ASwitch resets to `modelValue` before the PATCH lands.
const ignoreSeriesStatus = ref(false)

watch(
    () => qMe.data.value?.preferences.home?.ignoreSeriesStatus ?? false,
    v => (ignoreSeriesStatus.value = v),
    { immediate: true }
)

async function saveHome(v: boolean) {
    try {
        await homeMutation.mutateAsync({ home: { ignoreSeriesStatus: v || null } })
    } catch {
        ignoreSeriesStatus.value = !v
        return
    }
    invalidateRecentlyRead()
    toast.show({ message: 'Saved Home preferences' })
}

const speedPatch = usersApi.usePatchPreferences()
const speedFormId = useId()

// The fields emit null when emptied.
const wholeNumber = (min: number, max: number) =>
    z
        .number()
        .nullable()
        .pipe(
            z
                .number({ error: 'Enter a number' })
                .int('Enter a whole number')
                .min(min, `Use at least ${min}`)
                .max(max, `Use at most ${max}`)
        )

// Defaults are stored as absent, so they follow future default changes.
const unlessDefault = (v: number, d: number) => (v === d ? null : v)

const speedForm = useForm({
    schema: z.object({
        wordsPerMinute: wholeNumber(50, 2000),
        secondsPerPage: wholeNumber(1, 600),
    }),
    initialValues: readingSpeed(),
    onSubmit: async values => {
        await speedPatch.mutateAsync({
            reading: {
                wordsPerMinute: unlessDefault(values.wordsPerMinute, DEFAULT_WORDS_PER_MINUTE),
                secondsPerPage: unlessDefault(values.secondsPerPage, DEFAULT_SECONDS_PER_PAGE),
            },
        })
        toast.show({ message: 'Saved reading speed' })
    },
})

watch(
    () => qMe.data.value?.preferences.reading,
    reading => speedForm.setValues(readingSpeed({ reading })),
    { immediate: true }
)
</script>

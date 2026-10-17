<template>
    <div class="settings-page max-w-[780px]">
        <APageHeader title="Scrobbling" class="mb-1.5" />

        <ACard ref="card" title="MangaBaka" tabindex="-1">
            <QueryError :query="qAccount" />
            <div v-if="qAccount.isLoading.value" class="flex justify-center py-6">
                <ASpinner />
            </div>
            <template v-else-if="account">
                <AAlert v-if="account.rejected" tone="warning" class="mb-3">
                    MangaBaka rejected your token. Reconnect to resume scrobbling.
                </AAlert>
                <p v-else-if="account.connected" class="text-sm">
                    Connected as <strong>{{ account.remote_name || 'MangaBaka user' }}</strong>
                </p>
                <form
                    v-if="!account.connected || account.rejected"
                    :id="formId"
                    novalidate
                    @submit="form.onSubmit"
                >
                    <p class="mb-3 text-sm">
                        Create a personal access token in
                        <a
                            href="https://mangabaka.org/my/settings/api-and-apps"
                            target="_blank"
                            rel="noopener"
                            class="a-focus text-primary rounded-sm font-medium hover:underline"
                        >
                            your MangaBaka settings</a
                        >.
                    </p>
                    <ATextField
                        v-bind="form.field('token')"
                        label="Token"
                        type="password"
                        autocomplete="off"
                    />
                    <QueryError :mutation="form.mutation" class="mt-3" />
                </form>
                <QueryError :mutation="mDisconnect" class="mt-3" />
            </template>
            <template v-if="account" #actions>
                <AButton
                    v-if="account.connected"
                    variant="text"
                    tone="danger"
                    :loading="mDisconnect.isPending.value"
                    @click="disconnect"
                >
                    Disconnect
                </AButton>
                <AButton
                    v-if="!account.connected || account.rejected"
                    type="submit"
                    :form="formId"
                    :loading="form.mutation.isPending.value"
                >
                    Connect
                </AButton>
            </template>
        </ACard>

        <template v-if="account?.connected">
            <ACard title="Options">
                <ASwitch
                    v-model="privateNew"
                    label="Add new entries as private"
                    :readonly="mPatch.isPending.value"
                    @update:model-value="savePrivate"
                />
                <QueryError :mutation="mPatch" />
                <template #actions>
                    <AButton
                        variant="tonal"
                        :leading-icon="IconSync"
                        :loading="mSyncAll.isPending.value"
                        @click="syncAll"
                    >
                        Sync all
                    </AButton>
                </template>
            </ACard>

            <ACard title="Libraries">
                <QueryError :query="qLibraries" />
                <p
                    v-if="qLibraries.isSuccess.value && !libraries.length"
                    class="text-fg-muted text-sm"
                >
                    No library allows scrobbling.
                </p>
                <ASwitch
                    v-for="library in libraries"
                    :key="library.id"
                    :model-value="libraryOn[library.id] ?? true"
                    :label="library.name"
                    :readonly="mPrefs.isPending.value"
                    @update:model-value="v => saveLibrary(library.id, v)"
                />
                <QueryError :mutation="mPrefs" />
            </ACard>
        </template>

        <ACard
            v-if="account?.connected || store.rows.length"
            ref="queueCard"
            title="Queue"
            tabindex="-1"
        >
            <ScrobbleQueue full />
            <ul v-if="store.held.length" class="flex flex-col">
                <li
                    v-for="r in store.held"
                    :key="r.content_id"
                    class="flex items-center gap-2 py-1 text-sm"
                >
                    <span class="min-w-0 flex-1 [overflow-wrap:anywhere]">
                        <RouterLink
                            :to="{ name: 'info-page', params: { id: r.content_id } }"
                            class="a-focus rounded-field font-medium hover:underline"
                        >
                            {{ r.title }}
                        </RouterLink>
                        <span class="text-fg-muted"> · On hold</span>
                    </span>
                    <AButton
                        variant="text"
                        size="sm"
                        class="-mr-3"
                        :loading="releasing.has(r.content_id)"
                        :aria-label="`Release ${r.title}`"
                        @click="release(r.content_id)"
                    >
                        Release
                    </AButton>
                </li>
            </ul>
            <p v-if="store.loaded && !store.rows.length" class="text-fg-muted text-sm">
                Nothing pending.
            </p>
        </ACard>

        <ACard v-if="recent.length || qRecent.isError.value" title="Recently sent">
            <ul class="grid gap-x-6 gap-y-3 sm:grid-cols-2">
                <li v-for="r in recent" :key="r.content_id">
                    <RouterLink
                        :to="{ name: 'info-page', params: { id: r.content_id } }"
                        class="a-focus rounded-field group flex items-center gap-3"
                    >
                        <ACover
                            :src="coverUrl({ id: r.content_id, cover_version: r.cover_version })"
                            alt=""
                            class="w-12 shrink-0"
                        />
                        <span class="flex min-w-0 flex-col">
                            <span
                                class="font-medium [overflow-wrap:anywhere] group-hover:underline"
                            >
                                {{ r.title }}
                            </span>
                            <time :datetime="r.sent_at" class="text-fg-muted text-sm">
                                {{ formatRelative(r.sent_at, now.getTime()) }}
                            </time>
                        </span>
                    </RouterLink>
                </li>
            </ul>
            <QueryError :query="qRecent" />
        </ACard>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { useNow } from '@vueuse/core'
import { computed, reactive, ref, useId, useTemplateRef, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { z } from 'zod'
import { showConfirmModal } from '@/components/ConfirmModal.vue'
import QueryError from '@/components/QueryError.vue'
import ScrobbleQueue from '@/pages/_layout/ScrobbleQueue.vue'
import { useScrobbleStore } from '@/stores/scrobbles'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import ACover from '@/ui/ACover.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconSync } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { coverUrl } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { scrobbleApi } from '@/utils/api/scrobble'
import { usersApi } from '@/utils/api/users'
import { RequestError } from '@/utils/fetch'
import { formatRelative } from '@/utils/format'
import { useForm } from '@/utils/forms'

useHead({ title: 'Scrobbling' })

const toast = useToast()
const store = useScrobbleStore()
const qMe = usersApi.useMe()
const qAccount = scrobbleApi.useAccount()
const account = computed(() => qAccount.data.value)
const mConnect = scrobbleApi.useConnect()
const mPatch = scrobbleApi.usePatch()
const mDisconnect = scrobbleApi.useDisconnect()
const mSyncAll = scrobbleApi.useSyncAll()
const mPrefs = usersApi.usePatchPreferences()
const card = useTemplateRef<{ $el: HTMLElement }>('card')
const queueCard = useTemplateRef<{ $el: HTMLElement }>('queueCard')
const formId = useId()
const qRecent = scrobbleApi.useRecent()
const recent = computed(() => qRecent.data.value ?? [])
const now = useNow({ interval: 30_000 })

const form = useForm({
    schema: z.object({ token: z.string().trim().min(1, 'Enter a token.') }),
    initialValues: { token: '' },
    onSubmit: async values => {
        try {
            await mConnect.mutateAsync(values.token)
        } catch (err) {
            // The token's problem, worded by the server.
            if (err instanceof RequestError && err.response?.status === 400) {
                form.errors.value = [
                    { code: 'custom', path: ['token'], message: RequestError.getMessage(err) },
                ]
                return
            }
            throw err
        }
        form.reset()
        // The form is gone; keep focus in the card.
        card.value?.$el.focus()
    },
})

async function disconnect() {
    const confirmed = await showConfirmModal({
        title: 'Disconnect MangaBaka',
        message: 'Pending scrobbles are dropped. Holds are kept.',
        confirmText: 'Disconnect',
        tone: 'danger',
    })
    if (confirmed) mDisconnect.mutate(undefined, { onSuccess: () => card.value?.$el.focus() })
}

// Local, since ASwitch resets to `modelValue` before the PATCH lands.
const privateNew = ref(false)
watch(
    () => account.value?.private_new ?? false,
    v => (privateNew.value = v),
    { immediate: true }
)

async function savePrivate(v: boolean) {
    try {
        await mPatch.mutateAsync({ private_new: v })
    } catch {
        privateNew.value = !v
    }
}

async function syncAll() {
    const res = await mSyncAll.mutateAsync().catch((err: unknown) => {
        toast.show({ message: `Couldn't sync: ${RequestError.getMessage(err)}`, tone: 'danger' })
    })
    if (res) toast.show({ message: `Queued ${res.count} series` })
}

// The libraries that allow scrobbling, except hidden ones.
const qLibraries = librariesApi.useList()
const libraries = computed(() =>
    (qLibraries.data.value ?? []).filter(
        l =>
            l.settings.scrobbling &&
            qMe.data.value?.preferences.libraries?.[l.id]?.visibility !== 'hide'
    )
)

const libraryOn = ref<Record<string, boolean>>({})
watch(
    () => qMe.data.value?.preferences.libraries,
    prefs => {
        libraryOn.value = Object.fromEntries(
            Object.entries(prefs ?? {}).map(([id, p]) => [id, p.scrobble !== false])
        )
    },
    { immediate: true }
)

async function saveLibrary(id: string, v: boolean) {
    libraryOn.value[id] = v
    try {
        // A missing key is on.
        await mPrefs.mutateAsync({ libraries: { [id]: { scrobble: v ? null : false } } })
    } catch {
        libraryOn.value[id] = !v
    }
}

const releasing = reactive(new Set<string>())
async function release(id: string) {
    releasing.add(id)
    try {
        await scrobbleApi.hold(id, false)
        await store.refresh()
        // The row and its button are gone.
        queueCard.value?.$el.focus()
    } catch (err) {
        toast.show({ message: `Couldn't release: ${RequestError.getMessage(err)}`, tone: 'danger' })
    } finally {
        releasing.delete(id)
    }
}
</script>

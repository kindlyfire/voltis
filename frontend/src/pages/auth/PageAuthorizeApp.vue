<template>
    <AuthCard :title="request ? 'Sign in to the app' : 'Invalid sign-in link'">
        <p v-if="handoff" role="status" class="text-fg-muted text-sm leading-normal">
            Continue in the Voltis app.
        </p>
        <div v-else-if="request && me" class="flex flex-col gap-4">
            <div class="flex flex-col gap-1 text-sm leading-normal">
                <p>
                    Sign in to Voltis on <strong>{{ request.client_name }}</strong> as
                    <strong>{{ me.username }}</strong
                    >?
                </p>
                <p class="text-fg-muted">Only continue if you started this in the Voltis app.</p>
            </div>
            <QueryError :mutation="appCode" />
            <QueryError :mutation="logout" />
            <div v-if="canSwitch">
                <!-- App.vue sends the signed-out visitor to login with a redirect back here. -->
                <AButton
                    variant="text"
                    tone="neutral"
                    size="sm"
                    :loading="logout.isPending.value"
                    @click="logout.mutate()"
                >
                    Use another account
                </AButton>
            </div>
        </div>
        <template #footer>
            <AButton v-if="handoff" ref="openButton" @click="openUrl(handoff)">Open Voltis</AButton>
            <template v-else>
                <AButton variant="text" tone="neutral" @click="openUrl(cancelUrl)">Cancel</AButton>
                <AButton v-if="request && me" :loading="appCode.isPending.value" @click="authorize">
                    Continue
                </AButton>
            </template>
        </template>
    </AuthCard>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, nextTick, ref, useTemplateRef } from 'vue'
import { useRoute } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import { authApi } from '@/utils/api/auth'
import { usersApi } from '@/utils/api/users'
import AuthCard from './AuthCard.vue'

useHead({ title: 'Sign in to the app' })

const cancelUrl = 'voltis://auth/callback?error=cancelled'

const route = useRoute()
const me = usersApi.useMe().data
const appCode = authApi.useAppCode()
const logout = authApi.useLogout()
const handoff = ref<string>()
const openButton = useTemplateRef<InstanceType<typeof AButton>>('openButton')

// Only presence is checked: the server validates both.
const request = computed(() => {
    const { code_challenge, client_name } = route.query
    const name = typeof client_name === 'string' ? client_name.trim() : ''
    if (typeof code_challenge !== 'string' || !code_challenge || !name) return null
    return { code_challenge, client_name: name }
})

// The browser's cookies are shared with the app's tab, so this may not be the wanted account.
const canSwitch = computed(() => me.value?.session_method !== 'proxy' && me.value?.can_logout)

// `AButton :href` opens a new tab; the app's scheme needs a same-tab navigation.
function openUrl(url: string) {
    window.location.href = url
}

function authorize() {
    if (!request.value) return
    appCode.mutate(request.value, {
        onSuccess: async ({ redirect_url }) => {
            handoff.value = redirect_url
            openUrl(redirect_url)
            await nextTick()
            openButton.value?.$el.focus()
        },
    })
}
</script>

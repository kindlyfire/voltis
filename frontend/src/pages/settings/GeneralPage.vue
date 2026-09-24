<template>
    <VContainer>
        <h1 class="mb-6 text-4xl">General</h1>

        <AQueryError :query="qSettings" />
        <AQueryError :query="qProxy" />

        <div v-if="!loaded && !qSettings.isError.value" class="flex justify-center py-8">
            <VProgressCircular indeterminate />
        </div>

        <VForm v-else-if="loaded" @submit="form.onSubmit">
            <VCard class="mb-6">
                <VCardTitle>Sign-in</VCardTitle>
                <VCardText class="space-y-2">
                    <ASwitch
                        :model-value="form.values.value.registration"
                        @update:model-value="form.setValue('registration', $event)"
                        label="Allow anyone to register"
                    />
                    <ASwitch
                        :model-value="form.values.value.passwordLogin"
                        @update:model-value="form.setValue('passwordLogin', $event)"
                        label="Allow username and password login"
                    />
                    <ASwitch
                        :model-value="form.values.value.autoCreate"
                        @update:model-value="form.setValue('autoCreate', $event)"
                        label="Create an account on first SSO or proxy login"
                    />
                    <AInput
                        :input="form.getInputProps('sessionMaxDays')"
                        label="Maximum SSO and proxy session lifetime (days)"
                        type="number"
                    />
                    <AInput
                        :input="form.getInputProps('publicUrl')"
                        label="Public URL"
                        placeholder="https://voltis.example"
                        hint="Externally reachable base URL. Required for SSO."
                        persistent-hint
                    />
                </VCardText>
            </VCard>

            <VCard class="mb-6">
                <VCardTitle>Single sign-on (OIDC)</VCardTitle>
                <VCardText class="space-y-2">
                    <ASwitch
                        :model-value="form.values.value.oidcEnabled"
                        @update:model-value="form.setValue('oidcEnabled', $event)"
                        label="Enable single sign-on"
                    />
                    <AInput :input="form.getInputProps('oidcIssuer')" label="Issuer URL" />
                    <AInput :input="form.getInputProps('oidcClientId')" label="Client ID" />
                    <AInput
                        :input="form.getInputProps('oidcClientSecret')"
                        label="Client secret"
                        type="password"
                        :placeholder="secretSet ? 'Stored — type to replace' : ''"
                        hint="Write-only: the stored secret is never shown."
                        persistent-hint
                    />
                    <VTextField
                        :model-value="callbackUrl"
                        label="Redirect URI (paste this into your provider)"
                        readonly
                        hide-details
                        append-inner-icon="mdi-content-copy"
                        @click:append-inner="copyCallback"
                    />
                    <AInput :input="form.getInputProps('oidcScopes')" label="Scopes" />
                    <AInput :input="form.getInputProps('oidcButtonLabel')" label="Button label" />
                    <AInput
                        :input="form.getInputProps('oidcUsernameClaim')"
                        label="Username claim"
                    />
                    <AInput :input="form.getInputProps('oidcGroupsClaim')" label="Groups claim" />
                    <ASwitch
                        :model-value="form.values.value.oidcAutoRedirect"
                        @update:model-value="form.setValue('oidcAutoRedirect', $event)"
                        label="Send users straight to the provider"
                    />
                </VCardText>
            </VCard>

            <VCard class="mb-6">
                <VCardTitle>Account linking</VCardTitle>
                <VCardText class="space-y-2">
                    <ASwitch
                        :model-value="form.values.value.matchUsername"
                        @update:model-value="form.setValue('matchUsername', $event)"
                        label="Link to the local account with the same username"
                    />
                    <ASwitch
                        :model-value="form.values.value.matchEmail"
                        @update:model-value="form.setValue('matchEmail', $event)"
                        label="Link to the local account with the same email"
                    />
                    <VAlert
                        v-if="form.values.value.matchEmail"
                        type="warning"
                        variant="tonal"
                        density="compact"
                    >
                        Email matching trusts the provider to own the address. Anyone who can
                        register a matching verified email there can claim the local account that
                        uses it, and local addresses are not verified.
                    </VAlert>
                    <AInput
                        :input="form.getInputProps('adminGroup')"
                        label="Admin group"
                        hint="Members of this group become admins. Empty disables the mapping."
                        persistent-hint
                    />
                </VCardText>
            </VCard>

            <VCard class="mb-6">
                <VCardTitle>Forwarded authentication</VCardTitle>
                <VCardText class="space-y-2">
                    <AInput
                        :input="form.getInputProps('proxyLogoutUrl')"
                        label="Proxy logout URL"
                        hint="Where proxy users go on logout. Empty hides the logout button."
                        persistent-hint
                    />
                    <VDivider class="my-4" />
                    <p class="mb-2 text-xs opacity-60">
                        Configured in the environment, not here: a wrong trusted range would let
                        anyone take over any account.
                    </p>
                    <VTable density="compact">
                        <tbody>
                            <tr>
                                <td>Status</td>
                                <td>{{ proxy?.enabled ? 'Enabled' : 'Disabled' }}</td>
                            </tr>
                            <tr>
                                <td>Trusted CIDRs</td>
                                <td>{{ proxy?.trusted_cidrs.join(', ') || '—' }}</td>
                            </tr>
                            <tr>
                                <td>User header</td>
                                <td>{{ proxy?.user_header || '—' }}</td>
                            </tr>
                            <tr>
                                <td>Email header</td>
                                <td>{{ proxy?.email_header || '—' }}</td>
                            </tr>
                            <tr>
                                <td>Groups header</td>
                                <td>{{ proxy?.groups_header || '—' }}</td>
                            </tr>
                        </tbody>
                    </VTable>
                </VCardText>
            </VCard>

            <AQueryError :mutation="form.mutation" />
            <VBtn type="submit" color="primary" :loading="form.mutation.isPending.value">
                Save settings
            </VBtn>
        </VForm>
    </VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, watch } from 'vue'
import { z } from 'zod'
import AInput from '@/components/AInput.vue'
import AQueryError from '@/components/AQueryError.vue'
import { settingsApi, settingValue } from '@/utils/api/settings'
import { useForm } from '@/utils/forms'
import ASwitch from './ASwitch.vue'

useHead({ title: 'General settings' })

const qSettings = settingsApi.useList()
const qProxy = settingsApi.useProxyAuth()
const update = settingsApi.useUpdate()

const proxy = computed(() => qProxy.data.value)
// Editing before the load lands would save defaults over the real settings.
const loaded = computed(() => !!qSettings.data.value)
const secretSet = computed(
    () => qSettings.data.value?.find(s => s.key === 'auth.oidc.client_secret')?.set === true
)

const form = useForm({
    schema: z.object({
        publicUrl: z.string(),
        registration: z.boolean(),
        passwordLogin: z.boolean(),
        autoCreate: z.boolean(),
        sessionMaxDays: z.coerce.number().int().min(1).max(3650),
        oidcEnabled: z.boolean(),
        oidcIssuer: z.string(),
        oidcClientId: z.string(),
        oidcClientSecret: z.string(),
        oidcScopes: z.string(),
        oidcButtonLabel: z.string().min(1),
        oidcUsernameClaim: z.string().min(1),
        oidcGroupsClaim: z.string().min(1),
        oidcAutoRedirect: z.boolean(),
        matchUsername: z.boolean(),
        matchEmail: z.boolean(),
        adminGroup: z.string(),
        proxyLogoutUrl: z.string(),
    }),
    initialValues: {
        publicUrl: '',
        registration: false,
        passwordLogin: true,
        autoCreate: true,
        sessionMaxDays: 30,
        oidcEnabled: false,
        oidcIssuer: '',
        oidcClientId: '',
        oidcClientSecret: '',
        oidcScopes: 'openid profile email',
        oidcButtonLabel: 'Sign in with SSO',
        oidcUsernameClaim: 'preferred_username',
        oidcGroupsClaim: 'groups',
        oidcAutoRedirect: false,
        matchUsername: false,
        matchEmail: false,
        adminGroup: '',
        proxyLogoutUrl: '',
    },
    onSubmit: async values => {
        await update.mutateAsync({
            'app.public_url': values.publicUrl,
            'auth.registration_enabled': values.registration,
            'auth.password_login_enabled': values.passwordLogin,
            'auth.external_auto_create': values.autoCreate,
            'auth.external_session_max_days': values.sessionMaxDays,
            'auth.oidc.enabled': values.oidcEnabled,
            'auth.oidc.issuer': values.oidcIssuer,
            'auth.oidc.client_id': values.oidcClientId,
            // An empty field leaves the stored secret alone.
            ...(values.oidcClientSecret
                ? { 'auth.oidc.client_secret': values.oidcClientSecret }
                : {}),
            'auth.oidc.scopes': values.oidcScopes.split(/[\s,]+/).filter(Boolean),
            'auth.oidc.button_label': values.oidcButtonLabel,
            'auth.oidc.username_claim': values.oidcUsernameClaim,
            'auth.oidc.groups_claim': values.oidcGroupsClaim,
            'auth.oidc.auto_redirect': values.oidcAutoRedirect,
            'auth.link.match_username': values.matchUsername,
            'auth.link.match_email': values.matchEmail,
            'auth.admin_group': values.adminGroup,
            'auth.proxy.logout_url': values.proxyLogoutUrl,
        })
        form.setValue('oidcClientSecret', '')
    },
})

const callbackUrl = computed(() => {
    const base = form.values.value.publicUrl.replace(/\/+$/, '')
    return base ? `${base}/api/auth/oidc/callback` : 'Set the public URL first'
})

function copyCallback() {
    void navigator.clipboard?.writeText(callbackUrl.value)
}

watch(
    () => qSettings.data.value,
    settings => {
        if (!settings) return
        form.setValues({
            publicUrl: settingValue(settings, 'app.public_url', ''),
            registration: settingValue(settings, 'auth.registration_enabled', false),
            passwordLogin: settingValue(settings, 'auth.password_login_enabled', true),
            autoCreate: settingValue(settings, 'auth.external_auto_create', true),
            sessionMaxDays: settingValue(settings, 'auth.external_session_max_days', 30),
            oidcEnabled: settingValue(settings, 'auth.oidc.enabled', false),
            oidcIssuer: settingValue(settings, 'auth.oidc.issuer', ''),
            oidcClientId: settingValue(settings, 'auth.oidc.client_id', ''),
            oidcClientSecret: '',
            oidcScopes: settingValue<string[]>(settings, 'auth.oidc.scopes', []).join(' '),
            oidcButtonLabel: settingValue(settings, 'auth.oidc.button_label', ''),
            oidcUsernameClaim: settingValue(settings, 'auth.oidc.username_claim', ''),
            oidcGroupsClaim: settingValue(settings, 'auth.oidc.groups_claim', ''),
            oidcAutoRedirect: settingValue(settings, 'auth.oidc.auto_redirect', false),
            matchUsername: settingValue(settings, 'auth.link.match_username', false),
            matchEmail: settingValue(settings, 'auth.link.match_email', false),
            adminGroup: settingValue(settings, 'auth.admin_group', ''),
            proxyLogoutUrl: settingValue(settings, 'auth.proxy.logout_url', ''),
        })
    },
    { immediate: true }
)
</script>

<template>
    <div class="settings-page max-w-[780px]">
        <APageHeader title="General" class="mb-1.5" />

        <QueryError :query="qSettings" />
        <QueryError :query="qProxy" />

        <div v-if="!loaded && !qSettings.isError.value" class="flex justify-center py-16">
            <ASpinner size="lg" />
        </div>

        <form v-else-if="loaded" novalidate class="flex flex-col gap-4" @submit="form.onSubmit">
            <ACard title="Sign-in">
                <div class="flex flex-col">
                    <ASwitch v-bind="form.field('registration')" label="Allow anyone to register" />
                    <ASwitch
                        v-bind="form.field('passwordLogin')"
                        label="Allow username and password login"
                    />
                    <ASwitch
                        v-bind="form.field('autoCreate')"
                        label="Create an account on first SSO or proxy login"
                    />
                </div>
                <ATextField
                    v-bind="form.field('sessionMaxDays')"
                    label="Maximum SSO and proxy session lifetime"
                    type="number"
                    inputmode="numeric"
                    min="1"
                    max="3650"
                    suffix="days"
                />
                <ATextField
                    v-bind="form.field('publicUrl')"
                    label="Public URL"
                    type="url"
                    placeholder="https://voltis.example"
                    hint="Externally reachable base URL. Required for SSO."
                />
            </ACard>

            <ACard title="Single sign-on (OIDC)">
                <ASwitch v-bind="form.field('oidcEnabled')" label="Enable single sign-on" />
                <!-- `inert` keeps the dimmed fields out of the tab order too. -->
                <div
                    class="flex flex-col gap-2.5 transition-opacity duration-200"
                    :class="{ 'opacity-45': !form.values.value.oidcEnabled }"
                    :inert="!form.values.value.oidcEnabled"
                >
                    <ATextField v-bind="form.field('oidcIssuer')" label="Issuer URL" type="url" />
                    <ATextField v-bind="form.field('oidcClientId')" label="Client ID" />
                    <ATextField
                        v-bind="form.field('oidcClientSecret')"
                        label="Client secret"
                        type="password"
                        autocomplete="off"
                        :placeholder="secretSet ? 'Stored, type to replace' : undefined"
                        hint="Write-only: the stored secret is never shown."
                    />
                    <ATextField
                        :model-value="callbackUrl"
                        label="Redirect URI (paste this into your provider)"
                        placeholder="Set the public URL first"
                        readonly
                    >
                        <template v-if="callbackUrl" #trailing>
                            <AIconButton
                                :icon="IconContentCopy"
                                label="Copy the redirect URI"
                                @click="copyCallback"
                            />
                        </template>
                    </ATextField>
                    <ATextField v-bind="form.field('oidcScopes')" label="Scopes" />
                    <ATextField v-bind="form.field('oidcButtonLabel')" label="Button label" />
                    <ATextField v-bind="form.field('oidcUsernameClaim')" label="Username claim" />
                    <ATextField v-bind="form.field('oidcGroupsClaim')" label="Groups claim" />
                    <ASwitch
                        v-bind="form.field('oidcAutoRedirect')"
                        label="Send users straight to the provider"
                    />
                </div>
            </ACard>

            <ACard title="Account linking">
                <div class="flex flex-col">
                    <ASwitch
                        v-bind="form.field('matchUsername')"
                        label="Link to the local account with the same username"
                    />
                    <ASwitch
                        v-bind="form.field('matchEmail')"
                        label="Link to the local account with the same email"
                    />
                </div>
                <AAlert v-if="form.values.value.matchEmail" tone="warning">
                    Email matching trusts the provider to own the address. Anyone who can register a
                    matching verified email there can claim a local account that has no password and
                    no linked logins. Accounts with a password must confirm it first.
                </AAlert>
                <ATextField
                    v-bind="form.field('adminGroup')"
                    label="Admin group"
                    hint="Members of this group become admins. Empty disables the mapping."
                />
            </ACard>

            <ACard title="Forwarded authentication">
                <ATextField
                    v-bind="form.field('proxyLogoutUrl')"
                    label="Proxy logout URL"
                    type="url"
                    hint="Where proxy users go on logout. Empty hides the logout button."
                />
                <p class="text-fg-muted mt-1.5 mb-0.5 text-[13px] leading-normal">
                    Configured in the environment, not here: a wrong trusted range would let anyone
                    take over any account.
                </p>
                <dl class="text-sm">
                    <div
                        v-for="row in proxyRows"
                        :key="row.label"
                        class="border-outline-variant flex justify-between gap-4 border-t px-1 py-2.75"
                    >
                        <dt>{{ row.label }}</dt>
                        <dd class="text-fg-muted min-w-0 text-end [overflow-wrap:anywhere]">
                            {{ row.value }}
                        </dd>
                    </div>
                </dl>
            </ACard>

            <QueryError :mutation="form.mutation" />
            <div class="flex justify-end">
                <AButton type="submit" size="lg" :loading="form.mutation.isPending.value">
                    Save settings
                </AButton>
            </div>
        </form>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, watch } from 'vue'
import { z } from 'zod'
import QueryError from '@/components/QueryError.vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconContentCopy } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { settingsApi, settingValue } from '@/utils/api/settings'
import { useForm } from '@/utils/forms'

useHead({ title: 'General settings' })

const qSettings = settingsApi.useList()
const qProxy = settingsApi.useProxyAuth()
const update = settingsApi.useUpdate()

const toast = useToast()

const proxy = computed(() => qProxy.data.value)
const proxyRows = computed(() => [
    { label: 'Status', value: proxy.value?.enabled ? 'Enabled' : 'Disabled' },
    { label: 'Trusted CIDRs', value: proxy.value?.trusted_cidrs.join(', ') || '—' },
    { label: 'User header', value: proxy.value?.user_header || '—' },
    { label: 'Email header', value: proxy.value?.email_header || '—' },
    { label: 'Groups header', value: proxy.value?.groups_header || '—' },
])
// Editing before the load lands would save defaults over the real settings.
const loaded = computed(() => !!qSettings.data.value)
const secretSet = computed(
    () => qSettings.data.value?.find(s => s.key === 'auth.oidc.client_secret')?.set === true
)

const form = useForm({
    schema: z
        .object({
            publicUrl: z.string(),
            registration: z.boolean(),
            passwordLogin: z.boolean(),
            autoCreate: z.boolean(),
            // The field emits null when emptied.
            sessionMaxDays: z
                .number()
                .nullable()
                .pipe(
                    z
                        .number({ error: 'Enter a number of days' })
                        .int('Enter a whole number of days')
                        .min(1, 'Use at least 1 day')
                        .max(3650, 'Use at most 3650 days')
                ),
            oidcEnabled: z.boolean(),
            oidcIssuer: z.string(),
            oidcClientId: z.string(),
            oidcClientSecret: z.string(),
            oidcScopes: z.string(),
            oidcButtonLabel: z.string(),
            oidcUsernameClaim: z.string(),
            oidcGroupsClaim: z.string(),
            oidcAutoRedirect: z.boolean(),
            matchUsername: z.boolean(),
            matchEmail: z.boolean(),
            adminGroup: z.string(),
            proxyLogoutUrl: z.string(),
        })
        // Only while SSO is on: otherwise the fields are inert and an error there can't be fixed.
        .superRefine((values, ctx) => {
            if (!values.oidcEnabled) return
            const required = {
                oidcButtonLabel: 'Enter a button label',
                oidcUsernameClaim: 'Enter a username claim',
                oidcGroupsClaim: 'Enter a groups claim',
            } as const
            for (const [key, message] of Object.entries(required)) {
                if (!values[key as keyof typeof required].trim()) {
                    ctx.addIssue({ code: 'custom', path: [key], message })
                }
            }
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
        toast.show({ message: 'Saved the settings' })
    },
})

const callbackUrl = computed(() => {
    const base = form.values.value.publicUrl.replace(/\/+$/, '')
    return base ? `${base}/api/auth/oidc/callback` : ''
})

async function copyCallback() {
    try {
        await navigator.clipboard.writeText(callbackUrl.value)
        toast.show({ message: 'Copied the redirect URI', tone: 'info' })
    } catch {
        toast.show({ message: 'Could not copy the redirect URI', tone: 'danger' })
    }
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

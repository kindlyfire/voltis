import { VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import GeneralPage from '@/pages/settings/GeneralPage.vue'
import type { ProxyAuthStatus, Setting } from '@/utils/api/types'

const settings = ref<Setting[] | undefined>(undefined)
const settingsError = ref(false)
const proxy = ref<ProxyAuthStatus | undefined>(undefined)
const update = vi.fn()

vi.mock('@/utils/api/settings', async () => {
    const actual =
        await vi.importActual<typeof import('@/utils/api/settings')>('@/utils/api/settings')
    return {
        settingValue: actual.settingValue,
        settingsApi: {
            useList: () => ({ data: settings, isError: settingsError, error: ref(null) }),
            useProxyAuth: () => ({ data: proxy, isError: ref(false), error: ref(null) }),
            useUpdate: () => ({
                mutateAsync: update,
                isPending: ref(false),
                isError: ref(false),
                error: ref(null),
            }),
        },
    }
})

const pass = { template: '<div><slot /></div>' }
const stubs = {
    VContainer: pass,
    VCard: pass,
    VCardTitle: pass,
    VCardText: pass,
    VAlert: pass,
    VDivider: { template: '<hr />' },
    VTable: pass,
    VProgressCircular: { template: '<div class="loading" />' },
    VForm: { template: '<form><slot /></form>' },
    VBtn: { template: '<button><slot /></button>' },
    VTextField: { template: '<input />' },
    AInput: { props: ['input'], template: '<input />' },
    AQueryError: { template: '<div />' },
    ASwitch: { props: ['modelValue', 'label'], template: '<input type="checkbox" />' },
}

function setting(key: string, value: Setting['value'], extra: Partial<Setting> = {}): Setting {
    return { key, type: 'string', value, secret: false, help: '', ...extra }
}

const loaded: Setting[] = [
    setting('app.public_url', 'https://voltis.example'),
    setting('auth.password_login_enabled', true, { type: 'bool' }),
    setting('auth.oidc.enabled', true, { type: 'bool' }),
    setting('auth.oidc.issuer', 'https://idp.example'),
    setting('auth.oidc.scopes', ['openid', 'email'], { type: 'string_list' }),
    setting('auth.oidc.client_secret', null, { type: 'secret', secret: true, set: true }),
    setting('auth.oidc.button_label', 'Company SSO'),
    setting('auth.oidc.username_claim', 'preferred_username'),
    setting('auth.oidc.groups_claim', 'groups'),
    setting('auth.external_session_max_days', 30, { type: 'int' }),
]

function render() {
    return mount(GeneralPage, { global: { stubs, plugins: [VueQueryPlugin, createHead()] } })
}

enableAutoUnmount(afterEach)

describe('GeneralPage', () => {
    beforeEach(() => {
        settings.value = undefined
        settingsError.value = false
        proxy.value = undefined
        update.mockReset()
    })

    it('does not offer the form before the settings load', () => {
        const wrapper = render()
        expect(wrapper.find('.loading').exists()).toBe(true)
        expect(wrapper.find('form').exists()).toBe(false)
    })

    it('keeps the form away when the load failed', () => {
        settingsError.value = true
        const wrapper = render()
        expect(wrapper.find('form').exists()).toBe(false)
        expect(wrapper.find('.loading').exists()).toBe(false)
    })

    it('saves the loaded values and leaves the stored secret alone', async () => {
        settings.value = loaded
        const wrapper = render()
        await nextTick()
        expect(wrapper.find('form').exists()).toBe(true)

        await wrapper.find('form').trigger('submit')
        await new Promise(resolve => setTimeout(resolve))
        expect(update).toHaveBeenCalledTimes(1)

        const patch = update.mock.calls[0][0]
        expect(patch).not.toHaveProperty('auth.oidc.client_secret')
        expect(patch['auth.oidc.scopes']).toEqual(['openid', 'email'])
        expect(patch['app.public_url']).toBe('https://voltis.example')
        expect(patch['auth.oidc.enabled']).toBe(true)
    })
})

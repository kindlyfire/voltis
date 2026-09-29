import { VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import PageLogin from '@/pages/auth/PageLogin.vue'
import type { Info } from '@/utils/api/misc'
import { clearSignedOut, isSignedOut, markSignedOut } from '@/utils/api/oidc'

const info = ref<Partial<Info> | undefined>(undefined)
const query = ref<Record<string, string>>({})

vi.mock('@/utils/api/misc', () => ({
    miscApi: { useInfo: () => ({ data: info }) },
}))
vi.mock('@/utils/api/users', () => ({
    usersApi: { useMe: () => ({ data: ref(null) }) },
}))
vi.mock('@/utils/api/auth', () => ({
    authApi: { useLogin: () => ({ mutateAsync: vi.fn() }) },
}))
vi.mock('vue-router', () => ({
    useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
    useRoute: () => ({ query: query.value }),
    RouterLink: { template: '<a><slot /></a>' },
}))

const stubs = { QueryError: { template: '<div />' } }

function render() {
    return mount(PageLogin, { global: { stubs, plugins: [VueQueryPlugin, createHead()] } })
}

enableAutoUnmount(afterEach)

describe('PageLogin', () => {
    beforeEach(() => {
        vi.restoreAllMocks()
        sessionStorage.clear()
        clearSignedOut()
        info.value = undefined
        query.value = {}
        Object.defineProperty(window, 'location', {
            value: { href: '' },
            writable: true,
            configurable: true,
        })
    })

    it('navigates to the provider when auto-redirect is on', async () => {
        query.value = { redirect: '/lists' }
        info.value = { oidc_enabled: true, oidc_auto_redirect: true, password_login_enabled: true }
        render()
        expect(window.location.href).toContain('/auth/oidc/login')
        expect(window.location.href).toContain('redirect=%2Flists')
    })

    it('stays on the page when ?error= is present but empty', async () => {
        query.value = { error: '' }
        info.value = { oidc_enabled: true, oidc_auto_redirect: true, password_login_enabled: true }
        render()
        expect(window.location.href).toBe('')
    })

    it('stays on the page for ?local=1', async () => {
        query.value = { local: '1' }
        info.value = { oidc_enabled: true, oidc_auto_redirect: true, password_login_enabled: true }
        render()
        expect(window.location.href).toBe('')
    })

    it.each([false, true])('allows explicit SSO when storage removal throws: %s', async blocked => {
        markSignedOut()
        if (blocked) {
            vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
                throw new DOMException('Storage blocked', 'SecurityError')
            })
        }
        info.value = { oidc_enabled: true, oidc_auto_redirect: true, password_login_enabled: true }
        const wrapper = render()
        expect(window.location.href).toBe('')

        await wrapper.find('button').trigger('click')
        expect(window.location.href).toBe('/api/auth/oidc/login')
        expect(isSignedOut()).toBe(false)
    })

    it('shows the callback error and hides the password form when disabled', async () => {
        query.value = { error: 'the ID token was rejected' }
        info.value = {
            oidc_enabled: true,
            oidc_auto_redirect: false,
            password_login_enabled: false,
            oidc_button_label: 'Company SSO',
        }
        const wrapper = render()
        expect(wrapper.find('[role=alert]').text()).toBe('the ID token was rejected')
        expect(wrapper.find('form').exists()).toBe(false)
        expect(wrapper.text()).toContain('Company SSO')
    })

    it('keeps the password form when SSO is off', async () => {
        info.value = {
            oidc_enabled: false,
            oidc_auto_redirect: false,
            password_login_enabled: true,
        }
        const wrapper = render()
        expect(wrapper.find('form').exists()).toBe(true)
        expect(window.location.href).toBe('')
    })
})

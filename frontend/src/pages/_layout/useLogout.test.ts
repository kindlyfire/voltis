import { VueQueryPlugin } from '@tanstack/vue-query'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { useLogout } from '@/pages/_layout/useLogout'
import type { LogoutResponse, Me } from '@/utils/api/types'

const me = ref<Partial<Me> | null>(null)
const mutateAsync = vi.fn<() => Promise<LogoutResponse>>()
const push = vi.fn()

vi.mock('@/utils/api/users', () => ({
    usersApi: { useMe: () => ({ data: me }) },
}))
vi.mock('@/utils/api/auth', () => ({
    authApi: { useLogout: () => ({ mutateAsync, isPending: ref(false) }) },
}))
vi.mock('vue-router', () => ({
    useRouter: () => ({ push }),
}))

const Host = defineComponent({
    setup() {
        const { canLogout, logout } = useLogout()
        return () =>
            canLogout.value ? h('button', { onClick: () => void logout() }, 'Logout') : null
    },
})

enableAutoUnmount(afterEach)

describe('useLogout', () => {
    beforeEach(() => {
        me.value = { can_logout: true }
        push.mockReset()
        mutateAsync.mockReset()
        Object.defineProperty(window, 'location', {
            value: { href: '' },
            writable: true,
            configurable: true,
        })
    })

    const render = () => mount(Host, { global: { plugins: [VueQueryPlugin] } })

    const settle = () => new Promise(resolve => setTimeout(resolve))

    it('hides the button when logout is unavailable', () => {
        me.value = { can_logout: false }
        expect(render().find('button').exists()).toBe(false)
    })

    it('pushes the local login route when logout has no redirect URL', async () => {
        mutateAsync.mockResolvedValue({ ok: true, redirect_url: '' })
        await render().find('button').trigger('click')
        await settle()
        expect(push).toHaveBeenCalledWith('/auth/login?local=1')
        expect(window.location.href).toBe('')
    })

    it('leaves the app for the proxy logout URL', async () => {
        mutateAsync.mockResolvedValue({ ok: true, redirect_url: 'https://sso.example/logout' })
        await render().find('button').trigger('click')
        await settle()
        expect(window.location.href).toBe('https://sso.example/logout')
        expect(push).not.toHaveBeenCalled()
    })
})

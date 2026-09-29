import { VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '@/App.vue'
import Layout from '@/pages/_layout/Layout.vue'
import PageLogin from '@/pages/auth/PageLogin.vue'
import AccountPage from '@/pages/settings/AccountPage.vue'
import { clearSignedOut, isSignedOut } from '@/utils/api/oidc'
import { queryClient } from '@/utils/misc'

vi.mock('@/utils/ws', () => ({
    ws: { connect: vi.fn(), send: vi.fn(), on: vi.fn(() => () => {}) },
}))
vi.mock('@/stores/scans', () => ({ useScanSync: vi.fn() }))
vi.mock('@/pages/_layout/useLayoutStore', () => ({
    useLayoutStore: () => ({
        navbarHidden: { value: false },
        sidebarPersistent: true,
        sidebarTemporary: { value: false },
        closeDrawer: () => {},
    }),
}))
vi.mock('@/components/ConfirmModal.vue', () => ({
    showConfirmModal: vi.fn(async () => true),
}))

const pass = { template: '<div><slot /></div>' }
const stubs = {
    AMenu: { template: '<div><slot name="trigger" /><slot /></div>' },
    AMenuItem: { template: '<button @click="$emit(\'select\')"><slot /></button>' },
    QueryError: { template: '<div />' },
    Libraries: true,
    ScanIndicator: true,
    SearchBox: true,
    ModalContainer: true,
}

function deferred() {
    let resolve!: () => void
    const promise = new Promise<void>(done => {
        resolve = done
    })
    return { promise, resolve }
}

let signedIn: boolean
let sessionMethod: string
let sessionResponse: Promise<void> | undefined
let signOutRequest: Promise<void> | undefined
let signOutResponse: Promise<void> | undefined
let signOutFailure: string | undefined
let signOutRevokes: boolean
const user = { id: 'u_1', username: 'alice', can_logout: true, permissions: [] }

const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).replace(/^\/api/, '')
    if (path === '/users/me') {
        const me = signedIn ? { ...user, session_method: sessionMethod } : null
        await sessionResponse
        return me ? Response.json(me) : Response.json({ error: 'Unauthorized' }, { status: 401 })
    }
    if (path === '/info') {
        return Response.json({
            oidc_enabled: true,
            oidc_auto_redirect: true,
            password_login_enabled: true,
            first_user_flow: false,
            registration_enabled: false,
        })
    }
    if (
        path === '/auth/logout' ||
        (path.endsWith('/identities/i_1') && init?.method === 'DELETE')
    ) {
        await signOutRequest
        if (signOutRevokes) signedIn = false
        await signOutResponse
        if (signOutFailure === 'transport') throw new TypeError('Connection lost')
        if (signOutFailure === 'invalid JSON') return new Response('{')
        if (signOutFailure === 'server error') {
            return Response.json({ error: 'Unavailable' }, { status: 503 })
        }
        return Response.json({ ok: true, redirect_url: '' })
    }
    if (path === '/auth/login') {
        signedIn = true
        return Response.json({ ok: true })
    }
    if (path.endsWith('/identities')) {
        return Response.json([
            { id: 'i_1', provider: 'oidc', subject: 'alice', created_at: '2026-01-01' },
        ])
    }
    return Response.json([])
})

enableAutoUnmount(afterEach)
afterEach(() => {
    queryClient.clear()
    vi.unstubAllGlobals()
})

beforeEach(() => {
    queryClient.clear()
    vi.restoreAllMocks()
    sessionStorage.clear()
    clearSignedOut()
    signedIn = true
    sessionMethod = 'password'
    sessionResponse = undefined
    signOutRequest = undefined
    signOutResponse = undefined
    signOutFailure = undefined
    signOutRevokes = true
    fetchMock.mockClear()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('location', { href: '' })
})

async function render(path = '/') {
    const router = createRouter({
        history: createMemoryHistory(),
        routes: [
            {
                path: '/',
                component: Layout,
                children: [
                    { path: '', component: pass },
                    { path: 'settings/account', component: AccountPage },
                ],
            },
            { path: '/auth/login', name: 'login', component: PageLogin },
            { path: '/auth/register', name: 'register', component: pass },
        ],
    })
    await router.push(path)
    await router.isReady()
    const paths: string[] = []
    router.afterEach(to => {
        paths.push(to.fullPath)
    })
    const errors = vi.fn()
    const wrapper = mount(App, {
        global: {
            stubs,
            plugins: [[VueQueryPlugin, { queryClient }], createHead(), router],
            config: { errorHandler: errors },
        },
    })
    await flushPromises()
    return { wrapper, router, paths, errors }
}

const actions = [
    { action: 'nav logout', path: '/', label: 'Logout', login: '/auth/login' },
    {
        action: 'self-unlink',
        path: '/settings/account',
        label: 'Unlink alice',
        // An expiry mid-use returns to the current page after signing in.
        login: '/auth/login?redirect=/settings/account',
    },
]

describe.each(actions)('$action with auto-redirect on', ({ path, label, login }) => {
    it('survives the login → home → login round trip while the session refetch is pending', async () => {
        const { wrapper, router, paths } = await render(path)
        const response = deferred()
        sessionResponse = response.promise
        await wrapper
            .findAll('button')
            .find(button => button.text() === label || button.attributes('aria-label') === label)!
            .trigger('click')
        await flushPromises()

        expect(paths).toEqual(['/auth/login?local=1', '/'])
        expect(queryClient.getQueryData(['users', 'me'])).toMatchObject(user)
        expect(window.location.href).toBe('')

        response.resolve()
        await flushPromises()

        expect(paths).toEqual(['/auth/login?local=1', '/', '/auth/login'])
        expect(router.currentRoute.value.fullPath).toBe('/auth/login')
        expect(wrapper.findComponent(PageLogin).exists()).toBe(true)
        expect(window.location.href).toBe('')
    })

    it('stays local when session invalidation arrives before the mutation response', async () => {
        const { wrapper, router } = await render(path)
        const response = deferred()
        signOutResponse = response.promise
        await wrapper
            .findAll('button')
            .find(button => button.text() === label || button.attributes('aria-label') === label)!
            .trigger('click')
        await flushPromises()
        expect(signedIn).toBe(false)

        await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
        await flushPromises()
        expect(router.currentRoute.value.fullPath).toBe(login)
        expect(window.location.href).toBe('')

        response.resolve()
        await flushPromises()
        expect(router.currentRoute.value.path).toBe('/auth/login')
        expect(window.location.href).toBe('')
    })

    it('sends the request and stays local when storage writes throw', async () => {
        const { wrapper, router, errors } = await render(path)
        vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
            throw new DOMException('Storage blocked', 'SecurityError')
        })
        await wrapper
            .findAll('button')
            .find(button => button.text() === label || button.attributes('aria-label') === label)!
            .trigger('click')
        await flushPromises()

        expect(signedIn).toBe(false)
        expect(router.currentRoute.value.path).toBe('/auth/login')
        expect(wrapper.findComponent(PageLogin).exists()).toBe(true)
        expect(window.location.href).toBe('')
        expect(isSignedOut()).toBe(true)
        expect(errors).not.toHaveBeenCalled()
    })

    it.each(['transport', 'invalid JSON', 'server error'])(
        'stays local after revocation followed by a %s failure',
        async failure => {
            const { wrapper, router, errors } = await render(path)
            signOutFailure = failure
            await wrapper
                .findAll('button')
                .find(
                    button => button.text() === label || button.attributes('aria-label') === label
                )!
                .trigger('click')
            await flushPromises()
            expect(signedIn).toBe(false)
            expect(errors).toHaveBeenCalled()

            await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
            await flushPromises()
            expect(router.currentRoute.value.fullPath).toBe(login)
            expect(wrapper.findComponent(PageLogin).exists()).toBe(true)
            expect(window.location.href).toBe('')
            expect(isSignedOut()).toBe(true)
        }
    )

    it('clears intent once a failed sign-out is confirmed to have left the session intact', async () => {
        const { wrapper, errors } = await render(path)
        signOutFailure = 'server error'
        signOutRevokes = false
        await wrapper
            .findAll('button')
            .find(button => button.text() === label || button.attributes('aria-label') === label)!
            .trigger('click')
        await flushPromises()
        expect(errors).toHaveBeenCalled()
        expect(isSignedOut()).toBe(true)

        await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
        await flushPromises()
        expect(isSignedOut()).toBe(false)
    })

    it.each([
        { started: 'before', finished: 'before' },
        { started: 'during', finished: 'before' },
        { started: 'before', finished: 'after' },
        { started: 'during', finished: 'after' },
    ])(
        'ignores an authenticated check started $started and finished $finished sign-out',
        async ({ started, finished }) => {
            const { wrapper, router } = await render(path)
            const check = deferred()
            const request = deferred()
            sessionResponse = check.promise
            signOutRequest = request.promise

            const refetch = () => {
                void queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
                return flushPromises()
            }
            if (started === 'before') await refetch()
            await wrapper
                .findAll('button')
                .find(
                    button => button.text() === label || button.attributes('aria-label') === label
                )!
                .trigger('click')
            await flushPromises()
            if (started === 'during') await refetch()
            sessionResponse = undefined

            let keptIntent = true
            if (finished === 'before') {
                check.resolve()
                await flushPromises()
                keptIntent = isSignedOut()
            }
            request.resolve()
            await flushPromises()
            check.resolve()
            await flushPromises()

            expect(keptIntent).toBe(true)
            expect(isSignedOut()).toBe(true)
            expect(router.currentRoute.value.path).toBe('/auth/login')
            expect(window.location.href).toBe('')
        }
    )
})

it('auto-redirects an expired session through the same App guard', async () => {
    const { router } = await render()
    signedIn = false
    await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/auth/login')
    expect(window.location.href).toBe('/api/auth/oidc/login')
})

it('auto-redirects an absent session', async () => {
    signedIn = false
    const { router } = await render()

    expect(router.currentRoute.value.path).toBe('/auth/login')
    expect(window.location.href).toBe('/api/auth/oidc/login')
})

it('auto-redirects a later expiry after signing out and signing back in with a password', async () => {
    const { wrapper, router } = await render()
    await wrapper
        .findAll('button')
        .find(button => button.text() === 'Logout')!
        .trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/auth/login')
    expect(window.location.href).toBe('')

    await wrapper.find('input[autocomplete="username"]').setValue('alice')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/')
    expect(signedIn).toBe(true)

    signedIn = false
    await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/auth/login')
    expect(window.location.href).toBe('/api/auth/oidc/login')
})

it.each([
    { source: 'proxy sign-in', method: 'proxy', reload: true },
    { source: 'sign-in in another tab', method: 'oidc', reload: false },
])('auto-redirects a later expiry after $source', async ({ method, reload }) => {
    let { wrapper, router } = await render()
    await wrapper
        .findAll('button')
        .find(button => button.text() === 'Logout')!
        .trigger('click')
    await flushPromises()
    expect(isSignedOut()).toBe(true)

    signedIn = true
    sessionMethod = method
    if (reload) {
        wrapper.unmount()
        queryClient.clear()
        ;({ wrapper, router } = await render())
    } else {
        await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
        await flushPromises()
    }
    expect(router.currentRoute.value.path).toBe('/')

    signedIn = false
    await queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/auth/login')
    expect(wrapper.findComponent(PageLogin).exists()).toBe(true)
    expect(window.location.href).toBe('/api/auth/oidc/login')
})

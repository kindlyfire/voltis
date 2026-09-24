import { beforeEach, describe, expect, it, vi } from 'vitest'
import { shouldAutoRedirect } from './oidc'

const info = { oidc_auto_redirect: true, first_user_flow: false }

let intent: typeof import('./oidc')

beforeEach(async () => {
    vi.restoreAllMocks()
    sessionStorage.clear()
    vi.resetModules()
    intent = await import('./oidc')
})

describe('shouldAutoRedirect', () => {
    it('redirects when auto-redirect is on', () => {
        expect(shouldAutoRedirect(info, false, undefined, false)).toBe(true)
    })

    it('stays put when the setting is off or info is missing', () => {
        expect(
            shouldAutoRedirect({ ...info, oidc_auto_redirect: false }, false, undefined, false)
        ).toBe(false)
        expect(shouldAutoRedirect(undefined, false, undefined, false)).toBe(false)
    })

    it('stays put whenever ?error= is present, even when empty', () => {
        expect(shouldAutoRedirect(info, true, undefined, false)).toBe(false)
    })

    it('stays put when asked for the local form', () => {
        expect(shouldAutoRedirect(info, false, '1', false)).toBe(false)
    })

    it('leaves the bootstrap admin able to register', () => {
        expect(
            shouldAutoRedirect({ ...info, first_user_flow: true }, false, undefined, false)
        ).toBe(false)
    })

    it('stays put after a deliberate sign-out, but not after an expiry', () => {
        expect(shouldAutoRedirect(info, false, undefined, true)).toBe(false)
        expect(shouldAutoRedirect(info, false, undefined, false)).toBe(true)
    })
})

describe('sign-out intent', () => {
    it('survives repeated reads and module reloads until sign-in', async () => {
        expect(intent.isSignedOut()).toBe(false)
        intent.markSignedOut()
        expect(intent.isSignedOut()).toBe(true)
        expect(intent.isSignedOut()).toBe(true)
        vi.resetModules()
        expect((await import('./oidc')).isSignedOut()).toBe(true)
        intent.clearSignedOut()
        expect(intent.isSignedOut()).toBe(false)
    })

    it.each(['getItem', 'setItem', 'removeItem'] as const)(
        'keeps a non-destructive memory fallback when %s throws',
        operation => {
            vi.spyOn(Storage.prototype, operation).mockImplementation(() => {
                throw new DOMException('Storage blocked', 'SecurityError')
            })
            expect(intent.isSignedOut()).toBe(false)
            intent.markSignedOut()
            expect(intent.isSignedOut()).toBe(true)
            expect(intent.isSignedOut()).toBe(true)
            intent.clearSignedOut()
            expect(intent.isSignedOut()).toBe(false)
            expect(intent.isSignedOut()).toBe(false)
        }
    )

    it('remembers a stored intent after storage becomes unreadable', () => {
        sessionStorage.setItem('signedOut', '1')
        expect(intent.isSignedOut()).toBe(true)
        vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
            throw new DOMException('Storage blocked', 'SecurityError')
        })
        expect(intent.isSignedOut()).toBe(true)
        expect(intent.isSignedOut()).toBe(true)
    })

    it('works when accessing sessionStorage itself throws', () => {
        vi.spyOn(window, 'sessionStorage', 'get').mockImplementation(() => {
            throw new DOMException('Storage blocked', 'SecurityError')
        })
        expect(intent.isSignedOut()).toBe(false)
        intent.markSignedOut()
        expect(intent.isSignedOut()).toBe(true)
        intent.clearSignedOut()
        expect(intent.isSignedOut()).toBe(false)
    })
})

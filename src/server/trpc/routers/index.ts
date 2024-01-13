import { areRegistrationsEnabled } from '../../utils/state'
import { publicProcedure, router } from '../trpc.js'
import { rAuth } from './auth'
import { rCollections } from './collections'
import { rItems } from './items'
import { rLibraries } from './libraries'
import { rScan } from './scan'

export const appRouter = router({
	items: rItems,
	collections: rCollections,
	auth: rAuth,
	libraries: rLibraries,
	scan: rScan,

	meta: publicProcedure.query(async opts => {
		const runtimeConfig = useRuntimeConfig(opts.ctx.event)
		const reg = await areRegistrationsEnabled(opts.ctx.event)
		return {
			guestAccess: runtimeConfig.guestAccess,
			forceUserCreation: reg.forced,
			registrationsEnabled: reg.enabled
		}
	})
})

export type AppRouter = typeof appRouter

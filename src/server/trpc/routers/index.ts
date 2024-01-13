import { User } from '../../models/user'
import { areRegistrationsEnabled } from '../../utils/state'
import { publicProcedure, router } from '../trpc.js'
import { rAuth } from './auth'
import { rCollections } from './collections'
import { rItems } from './items'
import { rLibraries } from './libraries'

export const appRouter = router({
	items: rItems,
	collections: rCollections,
	auth: rAuth,
	libraries: rLibraries,

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

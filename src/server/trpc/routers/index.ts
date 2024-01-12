import { router } from '../trpc.js'
import { collectionsRouter } from './collections'
import { itemsRouter } from './items'

export const appRouter = router({
	items: itemsRouter,
	collections: collectionsRouter
})

export type AppRouter = typeof appRouter

import { z } from 'zod'
import { maybePublicProcedure, router } from '../trpc'

export const rCustomLists = router({
	query: maybePublicProcedure.query(async opts => {
		return []
	}),

	get: maybePublicProcedure
		.input(
			z.object({
				id: z.string()
			})
		)
		.query(async opts => {
			return []
		})
})

import { z } from 'zod'
import { maybePublicProcedure, router } from '../trpc.js'
import { Collection } from '../../models/collection'

export const rCollections = router({
	query: maybePublicProcedure.input(z.object({})).query(async ({ input }) => {
		const collections = await Collection.findAll()
		return collections.map(c => c.toJSON())
	}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return Collection.findByPk(input.id).then(c => c?.toJSON() ?? null)
		})
})

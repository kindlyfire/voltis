import { z } from 'zod'
import { maybePublicProcedure, router } from '../trpc.js'
import { Collection } from '../../models/collection'
import { search } from '@orama/orama'
import { Op } from 'sequelize'

export const rCollections = router({
	query: maybePublicProcedure
		.input(
			z.object({
				title: z.string().nullish()
			})
		)
		.query(async ({ input }) => {
			const index = await createSearchIndex()
			const results = await search(index, {
				term: input.title ?? undefined,
				boost: { title: 2 },
				limit: 10
			})
			const items = await Collection.findAll({
				where: {
					id: {
						[Op.in]: results.hits.map(r => r.document.id)
					}
				}
			})
			const sortedItems = results.hits
				.map(r => items.find(i => i.id === r.document.id)!)
				.filter(i => i != null)
			return sortedItems.map(c => c.toJSON())
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return Collection.findByPk(input.id).then(c => c?.toJSON() ?? null)
		})
})

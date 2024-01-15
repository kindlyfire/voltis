import { z } from 'zod'
import { maybePublicProcedure, router } from '../trpc.js'
import { Collection } from '../../models/collection'
import { search } from '@orama/orama'
import { Op } from 'sequelize'

export const rCollections = router({
	query: maybePublicProcedure
		.input(
			z.object({
				title: z.string().nullish(),
				libraryIds: z.array(z.string()).min(1).max(100).nullish(),
				limit: z.number().int().min(1).max(100).default(100)
			})
		)
		.query(async ({ input }) => {
			let titleSearchIds: string[] = []
			if (input.title) {
				const index = await createSearchIndex()
				const results = await search(index, {
					term: input.title ?? undefined,
					boost: { title: 2 },
					limit: input.limit
				})
				titleSearchIds = results.hits.map(r => r.document.id as string)
			}
			const collections = await Collection.findAll({
				where: {
					...(input.title ? { id: { [Op.in]: titleSearchIds } } : {}),
					...(input.libraryIds
						? { libraryId: { [Op.in]: input.libraryIds } }
						: {})
				},
				limit: input.limit
			})
			const sortedCollections = input.title
				? titleSearchIds
						.map(id => collections.find(i => i.id === id)!)
						.filter(i => i != null)
				: collections
			return sortedCollections.map(c => c.toJSON())
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return Collection.findByPk(input.id).then(c => c?.toJSON() ?? null)
		})
})

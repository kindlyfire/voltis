import { z } from 'zod'
import { maybePublicProcedure, router } from '../../trpc.js'
import { search } from '@orama/orama'
import { getSearchIndex } from '../../../utils/search-index'
import { prisma } from '../../../database'

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
				const index = await getSearchIndex()
				const results = await search(index, {
					term: input.title ?? undefined,
					boost: { title: 2 },
					limit: input.limit
				})
				titleSearchIds = results.hits.map(r => r.document.id as string)
			}
			const collections = await prisma.collection.findMany({
				where: {
					...(input.title ? { id: { in: titleSearchIds } } : {}),
					...(input.libraryIds ? { libraryId: { in: input.libraryIds } } : {})
				},
				take: input.limit
			})
			const sortedCollections = input.title
				? titleSearchIds
						.map(id => collections.find(i => i.id === id)!)
						.filter(i => i != null)
				: collections
			return sortedCollections
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return await prisma.collection.findById(input.id)
		})
})

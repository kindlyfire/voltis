import { z } from 'zod'
import { publicProcedure, router } from '../trpc.js'
import { Item } from '../../models/item'

export const itemsRouter = router({
	query: publicProcedure.input(z.object({})).query(async ({ input }) => {
		const items = await Item.findAll()
		return items.map(c => c.toJSON())
	}),

	get: publicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return Item.findByPk(input.id).then(c => c?.toJSON() ?? null)
		}),

	list: publicProcedure
		.input(
			z.object({
				collectionId: z.string()
			})
		)
		.query(async ({ input }) => {
			const items = await Item.findAll({
				where: {
					collectionId: input.collectionId
				}
			})

			// console.log(items.map(i => i.toJSON()))

			return items
				.sort((a, b) => {
					let i = 0
					while (true) {
						if (i >= a.sortValue.length) return 1
						if (i >= b.sortValue.length) return -1
						if (a.sortValue[i] < b.sortValue[i]) return 1
						if (a.sortValue[i] > b.sortValue[i]) return -1
						i++
					}
				})
				.map(c => c.toJSON())
		})
})

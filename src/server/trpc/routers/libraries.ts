import { z } from 'zod'
import { adminProcedure, maybePublicProcedure, router } from '../trpc.js'
import { Library } from '../../models/library'
import { Item } from '../../models/item'
import { Op } from 'sequelize'

export const rLibraries = router({
	query: maybePublicProcedure
		.input(z.object({}))
		.query(async ({ input, ctx }) => {
			const libraries = await Library.findAll()
			return libraries.map(c => c.export(ctx.user))
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input, ctx }) => {
			return Library.findByPk(input.id).then(c => c?.export(ctx.user))
		}),

	create: adminProcedure
		.input(
			z.object({
				name: z.string(),
				matcher: z.string(),
				paths: z.array(z.string())
			})
		)
		.mutation(async ({ input, ctx }) => {
			const library = await Library.create(input)
			return library.export(ctx.user)
		}),

	update: adminProcedure
		.input(
			z.object({
				id: z.string(),
				name: z.string(),
				matcher: z.string(),
				paths: z.array(z.string())
			})
		)
		.mutation(async ({ input, ctx }) => {
			const library = await Library.findByPk(input.id)
			if (!library) {
				throw new Error('Library not found')
			}
			await library.update(input)
			return library.export(ctx.user)
		}),

	delete: adminProcedure
		.input(z.object({ id: z.string() }))
		.mutation(async ({ input }) => {
			const library = await Library.findByPk(input.id, {
				include: {
					association: Library.associations.collections,
					attributes: ['id']
				}
			})
			if (!library) {
				throw new Error('Library not found')
			}
			await Item.destroy({
				where: {
					collectionId: {
						[Op.in]: library.collections!.map(c => c.id)
					}
				}
			})
			for (const collection of library.collections!) {
				await collection.destroy()
			}
			await library.destroy()
			return true
		})
})
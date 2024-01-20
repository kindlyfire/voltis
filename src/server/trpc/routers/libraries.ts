import { z } from 'zod'
import { adminProcedure, maybePublicProcedure, router } from '../trpc.js'
import { resetSearchIndex } from '../../utils/search-index'
import { prisma } from '../../database'
import { dbUtils } from '../../database/utils'

export const rLibraries = router({
	query: maybePublicProcedure
		.input(z.object({}))
		.query(async ({ input, ctx }) => {
			const libraries = await prisma.library.findMany({
				include: {
					_count: {
						select: { DiskCollection: true }
					}
				}
			})

			return libraries.map(c => {
				return {
					...c,
					_count: undefined,
					collectionCount: c._count.DiskCollection
				}
			})
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input, ctx }) => {
			return await prisma.library.findById(input.id)
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
			const library = await prisma.library.create({
				data: {
					id: dbUtils.createId(),
					name: input.name,
					type: 'comic',
					paths: input.paths
				}
			})
			return library
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
			const library = await prisma.library.update({
				where: { id: input.id },
				data: {
					name: input.name,
					type: 'comic',
					paths: input.paths
				}
			})
			return library
		}),

	delete: adminProcedure
		.input(z.object({ id: z.string() }))
		.mutation(async ({ input }) => {
			await prisma.library.delete({ where: { id: input.id } })
			resetSearchIndex()
			return true
		})
})

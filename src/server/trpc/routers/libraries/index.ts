import { z } from 'zod'
import { adminProcedure, maybePublicProcedure, router } from '../../trpc.js'
import { resetSearchIndex } from '../../../utils/search-index'
import { prisma } from '../../../database'
import { dbUtils } from '../../../database/utils'

export const rLibraries = router({
	query: maybePublicProcedure
		.input(z.object({}))
		.query(async ({ input, ctx }) => {
			const libraries = await prisma.dataSource.findMany({
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
			return await prisma.dataSource.findById(input.id)
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
			const dataSource = await prisma.dataSource.create({
				data: {
					id: dbUtils.createId(),
					name: input.name,
					type: 'comic',
					paths: input.paths
				}
			})
			return dataSource
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
			const dataSource = await prisma.dataSource.update({
				where: { id: input.id },
				data: {
					name: input.name,
					type: 'comic',
					paths: input.paths
				}
			})
			return dataSource
		}),

	delete: adminProcedure
		.input(z.object({ id: z.string() }))
		.mutation(async ({ input }) => {
			await prisma.dataSource.delete({ where: { id: input.id } })
			resetSearchIndex()
			return true
		})
})

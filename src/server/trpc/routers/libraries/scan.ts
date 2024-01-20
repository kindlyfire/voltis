import { z } from 'zod'
import { adminProcedure, router } from '../../trpc'
import { TRPCError } from '@trpc/server'
import { scanLibrary } from '../../../scanning/scanner'
import { prisma } from '../../../database'

export const rScan = router({
	scanLibraries: adminProcedure
		.input(
			z.object({
				libraryIds: z.array(z.string()).min(1)
			})
		)
		.mutation(async opts => {
			const libraries = await prisma.library.findMany({
				where: { id: { in: opts.input.libraryIds } }
			})
			if (libraries.length !== opts.input.libraryIds.length) {
				throw new TRPCError({
					code: 'NOT_FOUND',
					message: 'Some libraries could not found'
				})
			}

			await Promise.all(libraries.map(lib => scanLibrary(lib)))

			return true
		})
})

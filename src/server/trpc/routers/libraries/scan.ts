import { z } from 'zod'
import { adminProcedure, router } from '../../trpc'
import { TRPCError } from '@trpc/server'
import { scanDataSources } from '../../../scanning/scanner'
import { prisma } from '../../../database'

export const rScan = router({
	scanDataSources: adminProcedure
		.input(
			z.object({
				dataSourceIds: z.array(z.string()).min(1)
			})
		)
		.mutation(async opts => {
			const dataSources = await prisma.dataSource.findMany({
				where: { id: { in: opts.input.dataSourceIds } }
			})
			if (dataSources.length !== opts.input.dataSourceIds.length) {
				throw new TRPCError({
					code: 'NOT_FOUND',
					message: 'Some libraries could not found'
				})
			}

			await scanDataSources(dataSources)

			return true
		})
})

import { z } from 'zod'
import { router, userProcedure } from '../trpc'
import { prisma } from '../../database'
import { dbUtils } from '../../database/utils'

export const rUser = router({
	update: userProcedure
		.input(
			z.object({
				username: z.string().optional(),
				email: z.string().email().optional(),
				password: z.string().optional()
			})
		)
		.mutation(async opts => {
			await prisma.user.update({
				where: {
					id: opts.ctx.user.id
				},
				data: {
					username: opts.input.username,
					email: opts.input.email,
					password: opts.input.password
						? await dbUtils.user.hashPassword(opts.input.password)
						: undefined
				}
			})
		})
})

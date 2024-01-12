import { z } from 'zod'
import { publicProcedure, router, userProcedure } from '../trpc'
import { User } from '../../models/user'
import { Op } from 'sequelize'
import { TRPCError } from '@trpc/server'

export const rAuth = router({
	login: publicProcedure
		.input(
			z.object({
				emailOrUsername: z.string().min(3),
				password: z.string()
			})
		)
		.mutation(async opts => {
			const user = await User.findOne({
				where: {
					[Op.or]: [
						{ email: opts.input.emailOrUsername },
						{ username: opts.input.emailOrUsername }
					]
				}
			})
			if (!user) {
				throw new TRPCError({
					code: 'UNAUTHORIZED'
				})
			}

			const passwordMatch = await user.checkPassword(opts.input.password)
			if (!passwordMatch) {
				throw new TRPCError({
					code: 'UNAUTHORIZED'
				})
			}

			const session = await user.createSession({
				lastSeenAt: new Date()
			})
			const runtimeConfig = useRuntimeConfig(opts.ctx.event)
			setCookie(
				opts.ctx.event,
				runtimeConfig.sessionCookieName,
				session.token,
				{
					httpOnly: true
				}
			)

			return user.export(user)
		}),

	me: userProcedure.query(async opts => {
		return opts.ctx.user.export(opts.ctx.user)
	})
})

import { z } from 'zod'
import { publicProcedure, router, userProcedure } from '../trpc'
import { User } from '../../models/user'
import { Op } from 'sequelize'
import { TRPCError } from '@trpc/server'
import { areRegistrationsEnabled } from '../../utils/state'

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
					httpOnly: true,
					maxAge: 60 * 60 * 24 * 30 // 30 days
				}
			)

			return user.export(user)
		}),

	me: userProcedure.query(async opts => {
		return opts.ctx.user.export(opts.ctx.user)
	}),

	register: publicProcedure
		.input(
			z.object({
				email: z.string().email(),
				username: z.string().min(3),
				password: z.string().min(8)
			})
		)
		.mutation(async opts => {
			const reg = await areRegistrationsEnabled(opts.ctx.event)
			if (!reg.enabled) {
				throw new TRPCError({
					code: 'FORBIDDEN',
					message: 'Registrations are not enabled.'
				})
			}

			const user = User.build({
				email: opts.input.email,
				username: opts.input.username,
				roles: reg.forced ? ['admin'] : []
			})
			await user.setPassword(opts.input.password)
			await user.save()

			const session = await user.createSession({
				lastSeenAt: new Date()
			})
			const runtimeConfig = useRuntimeConfig(opts.ctx.event)
			setCookie(
				opts.ctx.event,
				runtimeConfig.sessionCookieName,
				session.token,
				{
					httpOnly: true,
					maxAge: 60 * 60 * 24 * 30 // 30 days
				}
			)

			return user.export(user)
		}),

	logout: publicProcedure.mutation(async opts => {
		const runtimeConfig = useRuntimeConfig(opts.ctx.event)
		deleteCookie(opts.ctx.event, runtimeConfig.sessionCookieName, {
			httpOnly: true
		})
		return true
	})
})

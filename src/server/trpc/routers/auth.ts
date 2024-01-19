import { z } from 'zod'
import { publicProcedure, router, userProcedure } from '../trpc'
import { TRPCError } from '@trpc/server'
import { areRegistrationsEnabled } from '../../utils/state'
import { prisma } from '../../database'
import { dbUtils } from '../../database/utils'
import { userVoter } from '../../database/voters'
import { Prisma } from '@prisma/client'

export const rAuth = router({
	login: publicProcedure
		.input(
			z.object({
				emailOrUsername: z.string().min(3),
				password: z.string()
			})
		)
		.mutation(async opts => {
			const user = await prisma.user.findFirst({
				where: {
					OR: [
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

			const passwordMatch = await dbUtils.user.checkPassword(
				user,
				opts.input.password
			)
			if (!passwordMatch) {
				throw new TRPCError({
					code: 'UNAUTHORIZED'
				})
			}

			const session = await prisma.userSession.create({
				data: {
					token: dbUtils.userSession.createToken(),
					userId: user.id
				}
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

			return userVoter.run(user, { user })
		}),

	me: userProcedure.query(async opts => {
		return userVoter.run(opts.ctx.user, { user: opts.ctx.user })
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

			const user = await prisma.user.create({
				data: {
					email: opts.input.email,
					username: opts.input.username,
					roles: reg.forced ? ['admin'] : [],
					password: await dbUtils.user.hashPassword(opts.input.password),
					preferences: {}
				}
			})

			const session = await prisma.userSession.create({
				data: {
					token: dbUtils.userSession.createToken(),
					userId: user.id
				}
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

			return userVoter.run(user, { user })
		}),

	logout: publicProcedure.mutation(async opts => {
		const runtimeConfig = useRuntimeConfig(opts.ctx.event)
		deleteCookie(opts.ctx.event, runtimeConfig.sessionCookieName, {
			httpOnly: true
		})
		return true
	})
})

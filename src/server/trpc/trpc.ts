import { TRPCError, initTRPC } from '@trpc/server'
import { Context } from './context.js'
import superjson from 'superjson'

const t = initTRPC.context<Context>().create({
	transformer: superjson
})

export const publicProcedure = t.procedure
export const router = t.router
export const middleware = t.middleware

export const maybePublicProcedure = publicProcedure.use(async opts => {
	const runtimeConfig = useRuntimeConfig(opts.ctx.event)
	if (!runtimeConfig.guestAccess && !opts.ctx.user) {
		throw new TRPCError({
			code: 'UNAUTHORIZED',
			message: 'You must be logged in.'
		})
	}
	return opts.next({
		ctx: {
			...opts.ctx,
			user: opts.ctx.user ?? null
		}
	})
})

export const userProcedure = publicProcedure.use(async opts => {
	if (!opts.ctx.user) {
		throw new TRPCError({
			code: 'UNAUTHORIZED',
			message: 'You must be logged in.'
		})
	}
	return opts.next({
		ctx: {
			...opts.ctx,
			user: opts.ctx.user!
		}
	})
})

export const adminProcedure = publicProcedure.use(async opts => {
	if (!opts.ctx.user) {
		throw new TRPCError({
			code: 'UNAUTHORIZED',
			message: 'You must be logged in.'
		})
	}
	if (!opts.ctx.user.roles.includes('admin')) {
		throw new TRPCError({
			code: 'UNAUTHORIZED',
			message: 'You must be an admin.'
		})
	}
	return opts.next({
		ctx: {
			...opts.ctx,
			user: opts.ctx.user!
		}
	})
})

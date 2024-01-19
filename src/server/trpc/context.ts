import { inferAsyncReturnType } from '@trpc/server'
import { H3Event } from 'h3'
import { prisma } from '../database'

export const createContext = async (event: H3Event) => {
	const runtimeConfig = useRuntimeConfig(event)
	const sessionToken = getCookie(event, runtimeConfig.sessionCookieName)
	const session = sessionToken
		? await prisma.userSession.findUnique({
				where: { token: sessionToken },
				include: { User: true }
		  })
		: null
	return {
		user: session?.User ?? null,
		userSession: session,
		event
	}
}

export type Context = inferAsyncReturnType<typeof createContext>

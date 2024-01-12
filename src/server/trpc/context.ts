import { inferAsyncReturnType } from '@trpc/server'
import { H3Event } from 'h3'
import { UserSession } from '../models/user-session'

export const createContext = async (event: H3Event) => {
	const runtimeConfig = useRuntimeConfig(event)
	const sessionToken = getCookie(event, runtimeConfig.sessionCookieName)
	const session = sessionToken
		? await UserSession.findOne({
				where: {
					token: sessionToken
				},
				include: [UserSession.associations.user]
		  })
		: null
	return {
		user: session?.user ?? null,
		userSession: session,
		event
	}
}

export type Context = inferAsyncReturnType<typeof createContext>

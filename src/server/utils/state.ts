import { H3Event } from 'h3'
import { prisma } from '../database'

export async function areRegistrationsEnabled(ev?: H3Event) {
	const runtimeConfig = useRuntimeConfig(ev)
	const forceUserCreation = (await prisma.user.count()) === 0
	return {
		enabled: runtimeConfig.registrationsEnabled || forceUserCreation,
		forced: forceUserCreation
	}
}

import { H3Event } from 'h3'
import { User } from '../models/user'

export async function areRegistrationsEnabled(ev?: H3Event) {
	const runtimeConfig = useRuntimeConfig(ev)
	const forceUserCreation = (await User.count()) === 0
	return {
		enabled: runtimeConfig.registrationsEnabled || forceUserCreation,
		forced: forceUserCreation
	}
}

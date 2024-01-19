import { User } from '@prisma/client'

export const userVoter = createVoter<User, { user?: User }>({
	defaults: {
		id: true,
		username: true,
		createdAt: true,
		roles: true,

		email: false,
		password: false,
		preferences: false,
		updatedAt: false
	},
	vote({ allow, context, object: user }) {
		if (context.user?.id == user.id) {
			allow(['email', 'preferences', 'updatedAt'])
		}
	}
})

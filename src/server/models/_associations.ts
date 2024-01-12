import { User } from './user'
import { UserSession } from './user-session'

export function associate() {
	User.hasMany(UserSession, {
		foreignKey: 'userId',
		onDelete: 'CASCADE',
		as: 'sessions'
	})

	UserSession.belongsTo(User, {
		foreignKey: 'userId',
		as: 'user'
	})
}

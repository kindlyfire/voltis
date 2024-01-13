import { Collection } from './collection'
import { Item } from './item'
import { Library } from './library'
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

	Library.hasMany(Collection, {
		foreignKey: 'libraryId',
		onDelete: 'CASCADE',
		as: 'collections'
	})
	Collection.belongsTo(Library, {
		foreignKey: 'libraryId',
		as: 'library'
	})

	Collection.hasMany(Item, {
		foreignKey: 'collectionId',
		onDelete: 'CASCADE',
		as: 'items'
	})
	Item.belongsTo(Collection, {
		foreignKey: 'collectionId',
		as: 'collection'
	})
}

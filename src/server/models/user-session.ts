import { nanoid } from 'nanoid'
import {
	Model,
	Sequelize,
	DataTypes,
	ForeignKey,
	Association,
	NonAttribute
} from 'sequelize'
import {
	InferAttributes,
	InferCreationAttributes,
	CreationOptional
} from 'sequelize'
import { User } from './user'

export class UserSession extends Model<
	InferAttributes<UserSession>,
	InferCreationAttributes<UserSession>
> {
	declare id: CreationOptional<string>
	declare userId: ForeignKey<User['id']>
	declare token: CreationOptional<string>
	declare lastSeenAt: CreationOptional<Date>
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>

	declare user?: NonAttribute<User>

	declare static associations: {
		user: Association<UserSession, User>
	}
}

export function init(sequelize: Sequelize) {
	UserSession.init(
		{
			id: {
				type: DataTypes.TEXT,
				allowNull: false,
				primaryKey: true,
				defaultValue: () => createId('i')
			},
			userId: typeText(),
			token: {
				type: DataTypes.TEXT,
				allowNull: false,
				unique: true,
				defaultValue: () => nanoid(32)
			},
			lastSeenAt: DataTypes.DATE,
			createdAt: DataTypes.DATE,
			updatedAt: DataTypes.DATE
		},
		{
			sequelize,
			modelName: 'UserSession',
			tableName: 'user_sessions'
		}
	)
}

const typeText = () => ({
	type: DataTypes.TEXT,
	allowNull: false
})

import {
	Model,
	Sequelize,
	DataTypes,
	Association,
	HasManyCreateAssociationMixin
} from 'sequelize'
import {
	InferAttributes,
	InferCreationAttributes,
	CreationOptional
} from 'sequelize'
import { UserSession } from './user-session'
import { createVoter } from '../utils/voter'
import bcrypt from 'bcrypt'

export interface UserPreferences {}

export class User extends Model<
	InferAttributes<User>,
	InferCreationAttributes<User>
> {
	declare id: CreationOptional<string>
	declare username: string
	declare email: string
	declare password: CreationOptional<string>
	declare preferences: CreationOptional<UserPreferences>
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>

	declare createSession: HasManyCreateAssociationMixin<UserSession, 'userId'>

	declare static associations: {
		sessions: Association<User, UserSession>
	}

	export(user: User) {
		return userVoter.run(this.toJSON(), {
			user
		})
	}

	async setPassword(password: string) {
		this.password = await bcrypt.hash(password, 10)
	}

	async checkPassword(password: string) {
		return await bcrypt.compare(password, this.password)
	}
}

const userVoter = createVoter<
	InferAttributes<User>,
	{ user?: InferAttributes<User> }
>({
	defaults: {
		id: true,
		username: true,
		createdAt: true,

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

export function init(sequelize: Sequelize) {
	User.init(
		{
			id: {
				type: DataTypes.TEXT,
				allowNull: false,
				primaryKey: true,
				defaultValue: () => createId('i')
			},
			username: {
				type: DataTypes.TEXT,
				allowNull: false,
				unique: true,
				validate: {
					isLowercase: true
				}
			},
			email: {
				type: DataTypes.TEXT,
				allowNull: false,
				unique: true,
				validate: {
					isEmail: true,
					isLowercase: true
				}
			},
			password: typeText(),
			preferences: {
				type: DataTypes.JSON,
				allowNull: false,
				defaultValue: {}
			},
			createdAt: DataTypes.DATE,
			updatedAt: DataTypes.DATE
		},
		{
			sequelize,
			modelName: 'User',
			tableName: 'users'
		}
	)
}

const typeText = () => ({
	type: DataTypes.TEXT,
	allowNull: false
})

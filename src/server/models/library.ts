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

export class Library extends Model<
	InferAttributes<Library>,
	InferCreationAttributes<Library>
> {
	declare id: CreationOptional<string>
	declare name: string
	declare matcher: string
	declare paths: string[]

	declare lastScanAt: CreationOptional<Date>
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>

	export(user?: User | undefined | null) {
		return libraryVoter.run(this.toJSON(), {
			user
		})
	}
}

const libraryVoter = createVoter<
	InferAttributes<Library>,
	{ user?: InferAttributes<User> | null }
>({
	defaults: {
		id: true,
		lastScanAt: true,
		matcher: true,
		name: true,

		paths: false,
		createdAt: false,
		updatedAt: false
	},
	vote({ allow, context }) {
		if (context.user?.roles.includes('admin')) {
			allow(['paths', 'createdAt', 'updatedAt'])
		}
	}
})

export function init(sequelize: Sequelize) {
	Library.init(
		{
			id: {
				type: DataTypes.TEXT,
				allowNull: false,
				primaryKey: true,
				defaultValue: () => createId('l')
			},
			name: typeText(),
			matcher: {
				type: DataTypes.TEXT,
				allowNull: false,
				validate: {
					isIn2(val: any) {
						if (val !== 'comic') {
							throw new Error('Invalid matcher')
						}
					}
				}
			},
			paths: {
				type: DataTypes.JSON,
				allowNull: false,
				defaultValue: [],
				validate: {
					isArray2(value: any) {
						if (!Array.isArray(value)) {
							throw new Error('paths must be an array')
						}
					}
				}
			},
			lastScanAt: DataTypes.DATE,
			createdAt: DataTypes.DATE,
			updatedAt: DataTypes.DATE
		},
		{
			sequelize,
			modelName: 'Library',
			tableName: 'libraries'
		}
	)
}

const typeText = () => ({
	type: DataTypes.TEXT,
	allowNull: false
})

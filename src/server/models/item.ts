import { Model, Sequelize, DataTypes } from 'sequelize'
import {
	InferAttributes,
	InferCreationAttributes,
	CreationOptional
} from 'sequelize'

export class Item extends Model<
	InferAttributes<Item>,
	InferCreationAttributes<Item>
> {
	declare id: CreationOptional<string>
	declare contentId: string
	declare collectionId: string
	declare name: string
	declare altNames: Array<string>
	declare path: string
	declare coverPath: string
	declare sortValue: Array<number>
	declare metadata: {}
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>
}

export function init(sequelize: Sequelize) {
	Item.init(
		{
			id: {
				type: DataTypes.TEXT,
				allowNull: false,
				primaryKey: true,
				defaultValue: () => createId('i')
			},
			contentId: typeText(),
			collectionId: typeText(),
			name: typeText(),
			altNames: {
				type: DataTypes.JSON,
				allowNull: false
			},
			path: typeText(),
			coverPath: typeText(),
			sortValue: {
				type: DataTypes.JSON,
				allowNull: false
			},
			metadata: {
				type: DataTypes.JSON,
				allowNull: false
			},
			createdAt: DataTypes.DATE,
			updatedAt: DataTypes.DATE
		},
		{
			sequelize,
			modelName: 'Item',
			tableName: 'items'
		}
	)
}

const typeText = () => ({
	type: DataTypes.TEXT,
	allowNull: false
})

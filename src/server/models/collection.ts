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
import { Metadata } from '../matcher/comics'
import { Library } from './library'
import { Item } from './item'

export class Collection extends Model<
	InferAttributes<Collection>,
	InferCreationAttributes<Collection>
> {
	declare id: CreationOptional<string>
	declare contentId: string
	declare libraryId: ForeignKey<Library['id']>
	declare kind: string
	declare name: string
	declare path: string
	declare coverPath: string
	declare missing: CreationOptional<boolean>
	declare categories: CreationOptional<any[]>
	declare metadata: CreationOptional<Partial<Metadata>>
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>

	declare library?: NonAttribute<Library>
	declare items?: NonAttribute<Item[]>
	declare static associations: {
		library: Association<Collection, Library>
		items: Association<Collection, Item>
	}
}

export function init(sequelize: Sequelize) {
	Collection.init(
		{
			id: {
				type: DataTypes.TEXT,
				allowNull: false,
				primaryKey: true,
				defaultValue: () => createId('c')
			},
			contentId: typeText(),
			libraryId: typeText(),
			kind: typeText(),
			name: typeText(),
			path: typeText(),
			coverPath: typeText(),
			missing: {
				type: DataTypes.BOOLEAN,
				allowNull: false,
				defaultValue: false
			},
			categories: {
				type: DataTypes.JSON,
				allowNull: false,
				defaultValue: () => []
			},
			metadata: {
				type: DataTypes.JSON,
				allowNull: false,
				defaultValue: () => ({})
			},
			createdAt: DataTypes.DATE,
			updatedAt: DataTypes.DATE
		},
		{
			sequelize,
			modelName: 'Collection',
			tableName: 'collections'
		}
	)
}

const typeText = () => ({
	type: DataTypes.TEXT,
	allowNull: false
})

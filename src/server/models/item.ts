import {
	Model,
	Sequelize,
	DataTypes,
	Association,
	ForeignKey,
	NonAttribute
} from 'sequelize'
import {
	InferAttributes,
	InferCreationAttributes,
	CreationOptional
} from 'sequelize'
import { Collection } from './collection'

export interface ItemMetadataSource {
	name: string
	remoteId: string | null
	overrideRemoteId?: string | null
	updatedAt: string | null
	customData?: Record<string, any>
	error?: null | {
		name: string
		message: string
		stack?: string
	}
}

export interface ItemMetadata {
	sources: ItemMetadataSource[]
}

export class Item extends Model<
	InferAttributes<Item>,
	InferCreationAttributes<Item>
> {
	declare id: CreationOptional<string>
	declare contentId: string
	declare collectionId: ForeignKey<Collection['id']>
	declare name: string
	declare altNames: Array<string>
	declare path: string
	declare coverPath: string
	declare sortValue: Array<number>
	declare metadata: CreationOptional<ItemMetadata>
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>

	declare collection?: NonAttribute<Collection>
	declare static associations: {
		collection: Association<Item, Collection>
	}

	async applyMetadataSourceFn(
		name: string,
		fn: (
			col: Collection,
			item: Item,
			source_: ItemMetadataSource
		) => Promise<ItemMetadataSource>
	) {
		const collection = await Collection.findByPk(this.collectionId)
		if (!collection) {
			throw new Error('Collection not found')
		}

		let source = this.metadata.sources.find(s => s.name === name)
		if (!source) {
			source = {
				name,
				updatedAt: null,
				remoteId: null
			}
		}
		source = await fn(collection, this, source)
		this.metadata = {
			...this.metadata,
			sources: [source, ...this.metadata.sources.filter(s => s.name !== name)]
		}
	}
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
				allowNull: false,
				get(this: Item) {
					return <ItemMetadata>{
						sources: [],
						...(this.getDataValue('metadata') as any)
					}
				}
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

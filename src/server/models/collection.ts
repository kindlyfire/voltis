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
import { Library } from './library'
import { Item } from './item'

export interface CollectionMetadataSource {
	name: string
	remoteId: string | null
	overrideRemoteId?: string | null
	updatedAt: Date | null
	data: Partial<CollectionMetadataData>
	customData?: Record<string, any>
	error?: null | {
		name: string
		message: string
		stack?: string
	}
}

export interface CollectionMetadataData {
	description?: string
	authors?: string[]
	pubStatus?: 'ongoing' | 'completed' | 'hiatus' | 'cancelled' | null
	pubYear?: number | null
	titles?: Array<{ [k: string]: string }>
}

export interface CollectionMetadata {
	sources: CollectionMetadataSource[]
	overrides: CollectionMetadataData
	merged: CollectionMetadataData
}

export class Collection extends Model<
	InferAttributes<Collection>,
	InferCreationAttributes<Collection>
> {
	declare id: CreationOptional<string>
	declare contentId: string
	declare libraryId: ForeignKey<Library['id']>
	declare kind: string
	declare name: string
	declare nameOverride: CreationOptional<string | null>
	declare path: string
	declare coverPath: string
	declare missing: CreationOptional<boolean>
	declare categories: CreationOptional<any[]>
	declare metadata: CreationOptional<CollectionMetadata>
	declare createdAt: CreationOptional<Date>
	declare updatedAt: CreationOptional<Date>

	declare library?: NonAttribute<Library>
	declare items?: NonAttribute<Item[]>
	declare static associations: {
		library: Association<Collection, Library>
		items: Association<Collection, Item>
	}

	mergeMetadata() {
		const obj: CollectionMetadataData = {
			authors: [],
			titles: []
		}
		for (const source of this.metadata.sources) {
			const d = source.data
			if (d.pubYear != null && obj.pubYear == null) obj.pubYear = d.pubYear
			if (d.pubStatus != null && obj.pubStatus == null)
				obj.pubStatus = d.pubStatus
			if (d.description != null && obj.description == null)
				obj.description = d.description
			if (d.authors != null) {
				const lowerCaseAuthors = obj.authors!.map(a => a.toLowerCase())
				for (const author of d.authors) {
					if (!lowerCaseAuthors.includes(author.toLowerCase())) {
						obj.authors!.push(author)
					}
				}
			}
			if (d.titles != null) {
				// TODO: Correct title merging logic
				for (const title of d.titles) {
					obj.titles!.push(title)
				}
			}
		}
		this.metadata = {
			...this.metadata,
			merged: obj
		}
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
			nameOverride: DataTypes.TEXT,
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
				defaultValue: () => ({}),
				get(this: Collection) {
					return <CollectionMetadata>{
						sources: [],
						merged: {},
						overrides: {},
						...(this.getDataValue('metadata') as any)
					}
				}
			},
			createdAt: DataTypes.DATE,
			updatedAt: DataTypes.DATE
		},
		{
			sequelize,
			modelName: 'Collection',
			tableName: 'collections',
			hooks: {
				beforeSave(instance, options) {
					instance.mergeMetadata()
				},
				beforeUpdate(instance, options) {
					instance.mergeMetadata()
				}
			}
		}
	)
}

const typeText = () => ({
	type: DataTypes.TEXT,
	allowNull: false
})

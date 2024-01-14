import { create, insertMultiple } from '@orama/orama'
import { Collection } from '../models/collection'

export async function createSearchIndex() {
	const index = await create({
		schema: {
			title: 'string'
		} as const
	})

	const collections = await Collection.findAll({
		attributes: ['id', 'name', 'nameOverride']
	})

	await insertMultiple(
		index,
		collections.map(c => {
			return {
				id: c.id,
				title: c.nameOverride || c.name
			}
		})
	)

	return index
}

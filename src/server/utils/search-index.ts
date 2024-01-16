import { create, insertMultiple } from '@orama/orama'
import { Collection } from '../models/collection'

let index: Awaited<ReturnType<typeof createSearchIndex>> | null = null
export async function getSearchIndex() {
	if (!index) {
		index = await createSearchIndex()
	}
	return index
}

export function resetSearchIndex() {
	index = null
}

async function createSearchIndex() {
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

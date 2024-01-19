import { create, insertMultiple } from '@orama/orama'
import { prisma } from '../database'

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

	const collections = await prisma.collection.findMany({
		select: {
			id: true,
			name: true,
			nameOverride: true
		}
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

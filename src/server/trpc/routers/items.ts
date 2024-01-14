import { z } from 'zod'
import { maybePublicProcedure, router } from '../trpc.js'
import { Item } from '../../models/item'
import { TRPCError } from '@trpc/server'
import {
	FileMetadataCustomData,
	fileMetadataFn
} from '../../scanning/comic/metadata-file'

export const rItems = router({
	query: maybePublicProcedure.input(z.object({})).query(async ({ input }) => {
		const items = await Item.findAll()
		return items.map(c => c.toJSON())
	}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return Item.findByPk(input.id).then(c => c?.toJSON() ?? null)
		}),

	list: maybePublicProcedure
		.input(
			z.object({
				collectionId: z.string()
			})
		)
		.query(async ({ input }) => {
			const items = await Item.findAll({
				where: {
					collectionId: input.collectionId
				}
			})

			return items
				.sort((a, b) => {
					let i = 0
					while (true) {
						if (i >= a.sortValue.length) return 1
						if (i >= b.sortValue.length) return -1
						if (a.sortValue[i] < b.sortValue[i]) return 1
						if (a.sortValue[i] > b.sortValue[i]) return -1
						i++
					}
				})
				.map(c => c.toJSON())
		}),

	getReaderData: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			const item = await Item.findByPk(input.id)
			if (!item) {
				throw new TRPCError({ code: 'NOT_FOUND' })
			}

			let fileSource = item.metadata.sources.find(s => s.name === 'file')
			if (!fileSource) {
				await item.applyMetadataSourceFn('file', fileMetadataFn)
				item.save()
				fileSource = item.metadata.sources.find(s => s.name === 'file')
			}
			if (!fileSource) {
				throw new TRPCError({ code: 'NOT_FOUND' })
			}

			return fileSource.customData! as FileMetadataCustomData
		})
})

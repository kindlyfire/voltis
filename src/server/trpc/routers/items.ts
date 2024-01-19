import { z } from 'zod'
import { maybePublicProcedure, router } from '../trpc.js'
import { TRPCError } from '@trpc/server'
import { prisma } from '../../database'
import { diskItemComicMetadataFn } from '../../scanning/comic/metadata-file'

export const rItems = router({
	query: maybePublicProcedure
		.input(
			z.object({
				collectionId: z.string().nullish(),
				inSameCollectionAs: z.string().nullish()
			})
		)
		.query(async ({ input }) => {
			let collectionId: string | undefined
			if (input.inSameCollectionAs) {
				const item = await prisma.item.findById(input.inSameCollectionAs)
				if (!item) {
					throw new TRPCError({ code: 'NOT_FOUND' })
				}
				collectionId = item.collectionId
			} else if (input.collectionId) {
				collectionId = input.collectionId
			}

			const items = await prisma.item.findMany({
				where: { collectionId }
			})

			return items.sort((a, b) => {
				let i = 0
				while (true) {
					if (i >= a.sortValue.length) return 1
					if (i >= b.sortValue.length) return -1
					if (a.sortValue[i] < b.sortValue[i]) return 1
					if (a.sortValue[i] > b.sortValue[i]) return -1
					i++
				}
			})
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return await prisma.item.findById(input.id)
		}),

	getReaderData: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			const item = await prisma.item.findUnique({
				where: { id: input.id },
				include: { Collection: true }
			})
			if (!item) {
				throw new TRPCError({ code: 'NOT_FOUND', message: 'Item not found.' })
			}

			const ditem = await prisma.diskItem.findFirst({
				where: { contentUri: item.contentUri }
			})
			if (!ditem) {
				throw new TRPCError({
					code: 'NOT_FOUND',
					message: 'No disk item found.'
				})
			}

			let fileSource = ditem.metadata?.comic
			if (!fileSource) {
				fileSource = await diskItemComicMetadataFn(ditem)
				await prisma.diskItem.update({
					where: { id: ditem.id },
					data: { metadata: { comic: fileSource } }
				})
			}
			if (!fileSource) {
				throw new TRPCError({ code: 'NOT_FOUND' })
			}

			return {
				collectionId: item.collectionId,
				pages: fileSource.files,
				suggestedMode: fileSource.suggestedMode ?? 'pages',
				chapterTitle: item.name,
				collectionTitle: item.Collection.name,
				diskItemId: ditem.id
			}
		})
})

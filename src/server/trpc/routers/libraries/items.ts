import { z } from 'zod'
import { maybePublicProcedure, router, userProcedure } from '../../trpc.js'
import { TRPCError } from '@trpc/server'
import { prisma } from '../../../database'
import { diskItemComicMetadataFn } from '../../../scanning/comic/metadata-file'
import { dbUtils } from '../../../database/utils'
import { Item, Prisma } from '@prisma/client'

export const rItems = router({
	query: maybePublicProcedure
		.input(
			z.object({
				collectionId: z.string().nullish(),
				inSameCollectionAs: z.string().nullish()
			})
		)
		.query(async opts => {
			let collectionId: string | undefined
			if (opts.input.inSameCollectionAs) {
				const item = await prisma.item.findById(opts.input.inSameCollectionAs)
				if (!item) {
					throw new TRPCError({ code: 'NOT_FOUND' })
				}
				collectionId = item.collectionId
			} else if (opts.input.collectionId) {
				collectionId = opts.input.collectionId
			}

			const items = await prisma.item.findMany({
				where: { collectionId },
				include: {
					UserItemData: opts.ctx.user
						? {
								where: { userId: opts.ctx.user.id }
						  }
						: false
				}
			})

			return sortItems(items).map(item => {
				return {
					...item,
					UserItemData: undefined,
					userData: item.UserItemData[0]
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
		.query(async ({ input, ctx }) => {
			const item = await prisma.item.findUnique({
				where: { id: input.id },
				include: {
					Collection: true,
					UserItemData: ctx.user
						? {
								where: {
									userId: ctx.user.id
								}
						  }
						: false
				}
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

			const userProgress = item.UserItemData?.at(0)
				? {
						page: item.UserItemData[0].progress?.page ?? 0,
						completed: item.UserItemData[0].completed
				  }
				: null

			return {
				collectionId: item.collectionId,
				pages: fileSource.files,
				suggestedMode: fileSource.suggestedMode ?? 'pages',
				chapterTitle: item.name,
				collectionTitle: item.Collection.name,
				diskItemId: ditem.id,
				userProgress
			}
		}),

	updateUserData: userProcedure
		.input(
			z.object({
				itemId: z.string(),
				progress: z.record(z.any()).nullish(),
				completed: z.boolean().nullish(),
				bookmarked: z.boolean().nullish()
			})
		)
		.mutation(async opts => {
			await prisma.userItemData.upsert({
				where: {
					userId_itemId: {
						userId: opts.ctx.user.id,
						itemId: opts.input.itemId
					}
				},
				create: {
					id: dbUtils.createId(),
					userId: opts.ctx.user.id,
					itemId: opts.input.itemId,
					progress: opts.input.progress ?? undefined,
					completed: opts.input.completed ?? undefined,
					bookmarked: opts.input.bookmarked ?? undefined
				},
				update: {
					progress: opts.input.completed
						? Prisma.DbNull
						: opts.input.progress ?? undefined,
					completed: opts.input.completed ?? undefined,
					bookmarked: opts.input.bookmarked ?? undefined
				}
			})
			return true
		}),

	bulkUpdateReadStatus: userProcedure
		.input(
			z.object({
				itemIds: z.array(z.string()),
				completed: z.boolean()
			})
		)
		.mutation(async opts => {
			const existing = await prisma.userItemData.findMany({
				where: {
					userId: opts.ctx.user.id,
					itemId: { in: opts.input.itemIds }
				},
				select: { itemId: true }
			})

			await prisma.$transaction([
				prisma.userItemData.updateMany({
					where: {
						userId: opts.ctx.user.id,
						itemId: { in: existing.map(e => e.itemId) }
					},
					data: {
						completed: opts.input.completed,
						progress: Prisma.DbNull
					}
				}),
				prisma.userItemData.createMany({
					data: opts.input.itemIds
						.filter(itemId => !existing.some(e => e.itemId === itemId))
						.map(itemId => ({
							id: dbUtils.createId(),
							userId: opts.ctx.user.id,
							itemId: itemId,
							completed: opts.input.completed
						}))
				})
			])
			return true
		})
})

export function sortItems<T extends Item>(items: T[]) {
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
}

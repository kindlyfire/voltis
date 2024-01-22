import { z } from 'zod'
import { maybePublicProcedure, router, userProcedure } from '../../trpc.js'
import { search } from '@orama/orama'
import { getSearchIndex } from '../../../utils/search-index'
import { prisma } from '../../../database'
import { dbUtils } from '../../../database/utils'
import { sortItems } from './items'

export const rCollections = router({
	query: maybePublicProcedure
		.input(
			z.object({
				title: z.string().nullish(),
				limit: z.number().int().min(1).max(100).default(100)
			})
		)
		.query(async opts => {
			let titleSearchIds: string[] = []
			if (opts.input.title) {
				const index = await getSearchIndex()
				const results = await search(index, {
					term: opts.input.title ?? undefined,
					boost: { title: 2 },
					limit: opts.input.limit
				})
				titleSearchIds = results.hits.map(r => r.document.id as string)
			}
			const collections = await prisma.collection.findMany({
				where: {
					...(opts.input.title ? { id: { in: titleSearchIds } } : {})
				},
				take: opts.input.limit,
				include: {
					UserCollectionData: opts.ctx.user
						? {
								where: { userId: opts.ctx.user.id }
						  }
						: false
				}
			})
			const sortedCollections = opts.input.title
				? titleSearchIds
						.map(id => collections.find(i => i.id === id)!)
						.filter(i => i != null)
				: collections
			return sortedCollections.map(collection => {
				return {
					...collection,
					UserCollectionData: undefined,
					userData: collection.UserCollectionData[0]
				}
			})
		}),

	get: maybePublicProcedure
		.input(z.object({ id: z.string() }))
		.query(async ({ input }) => {
			return await prisma.collection.findById(input.id)
		}),

	updateUserData: userProcedure
		.input(
			z.object({
				collectionId: z.string(),
				notes: z.string().nullish(),
				rating: z.number().int().min(0).max(10).nullish()
			})
		)
		.mutation(async opts => {
			return await prisma.userCollectionData.upsert({
				where: {
					userId_collectionId: {
						userId: opts.ctx.user.id,
						collectionId: opts.input.collectionId
					}
				},
				create: {
					id: dbUtils.createId(),
					userId: opts.ctx.user.id,
					...opts.input
				},
				update: opts.input
			})
		}),

	getReadStatus: userProcedure
		.input(z.object({ collectionId: z.string() }))
		.query(async opts => {
			let items = await prisma.item.findMany({
				where: {
					collectionId: opts.input.collectionId
				},
				include: {
					UserItemData: {
						where: { userId: opts.ctx.user.id },
						select: {
							completed: true,
							progress: true
						}
					}
				}
			})

			// Result is items from earliest to latest
			items = sortItems(items).toReversed()
			const lastRead = items.findLast(i =>
				i.UserItemData.length > 0
					? i.UserItemData[0].completed || i.UserItemData[0].progress
					: false
			)
			const isReadingLast =
				lastRead && lastRead?.UserItemData[0].progress !== null

			const nextRead = isReadingLast
				? lastRead
				: items.at(lastRead ? items.indexOf(lastRead) + 1 : 0)

			return {
				reading: nextRead?.id ?? null,
				progress: lastRead?.UserItemData[0].progress ?? null
			}
		})
})

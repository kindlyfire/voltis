import { z } from 'zod'
import { maybePublicProcedure, router, userProcedure } from '../trpc'
import { prisma } from '../../database'
import { dbUtils } from '../../database/utils'

const zReadingStatusEnum = z.enum([
	'custom',
	'reading',
	'plan to read',
	'on hold',
	're-reading',
	'dropped'
])

export const rCustomLists = router({
	query: maybePublicProcedure.query(async opts => {
		return []
	}),

	get: maybePublicProcedure
		.input(
			z.object({
				id: z.string()
			})
		)
		.query(async opts => {
			return []
		}),

	getUserLibraryItems: userProcedure
		.input(
			z.object({
				type: zReadingStatusEnum
			})
		)
		.query(async opts => {
			const v = await prisma.userCustomList_Collection.findMany({
				where: {
					UserCustomList: {
						type: opts.input.type
					}
				},
				include: {
					Collection: true
				}
			})

			return v
		}),

	getUserListsForCollection: userProcedure
		.input(
			z.object({
				id: z.string(),
				types: z.array(zReadingStatusEnum).nullish()
			})
		)
		.query(async opts => {
			const v = await prisma.userCustomList.findMany({
				where: {
					userId: opts.ctx.user.id,
					type: opts.input.types
						? {
								in: opts.input.types
						  }
						: undefined,
					UserCustomList_Collection: {
						some: {
							collectionId: opts.input.id
						}
					}
				}
			})
			return v
		}),

	addCollectionToLibrary: userProcedure
		.input(
			z.object({
				id: z.string(),
				type: zReadingStatusEnum.exclude(['custom'])
			})
		)
		.mutation(async opts => {
			let list = await prisma.userCustomList.findFirst({
				where: {
					userId: opts.ctx.user.id,
					type: opts.input.type
				}
			})
			if (!list) {
				list = await prisma.userCustomList.create({
					data: {
						id: dbUtils.createId(),
						type: opts.input.type,
						userId: opts.ctx.user.id,
						name: opts.input.type
					}
				})
			}

			const [_, item] = await prisma.$transaction([
				prisma.userCustomList_Collection.deleteMany({
					where: {
						collectionId: opts.input.id,
						UserCustomList: {
							type: {
								in: Object.keys(zReadingStatusEnum.exclude(['custom']).Values)
							}
						}
					}
				}),
				prisma.userCustomList_Collection.create({
					data: {
						id: dbUtils.createId(),
						collectionId: opts.input.id,
						userCustomListId: list.id,
						order: 0
					}
				})
			])

			return item
		}),

	deleteCollection: userProcedure
		.input(
			z.object({
				id: z.string(),
				fromLibary: z.boolean().nullish(),
				customListId: z.string().nullish()
			})
		)
		.mutation(async opts => {
			const v = await prisma.userCustomList_Collection.deleteMany({
				where: {
					collectionId: opts.input.id,
					UserCustomList: {
						userId: opts.ctx.user.id,
						type: opts.input.fromLibary
							? {
									in: Object.keys(zReadingStatusEnum.exclude(['custom']).Values)
							  }
							: undefined,
						id: opts.input.customListId ?? undefined
					}
				}
			})

			return v
		})
})

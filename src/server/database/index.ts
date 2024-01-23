import { PrismaClient, Prisma } from '@prisma/client'
import { Kysely, PostgresDialect } from 'kysely'
import pg from 'pg'
import { DB } from '../database/types-kysely'

const u = new URL(process.env.DB_URI!)
const dialect = new PostgresDialect({
	pool: new pg.Pool({
		database: u.pathname.slice(1),
		host: u.hostname,
		user: u.username,
		password: u.password,
		port: Number(u.port),
		max: 10
	})
})

export const prisma = new PrismaClient().$extends({
	model: {
		$allModels: {
			async findById<T>(
				this: T,
				id: string
			): Promise<Prisma.Result<T, { id: string }, 'findUnique'> | null> {
				const context = Prisma.getExtensionContext(this)
				return await (context as any).findUnique({ where: { id } })
			}
		}
	}
})

export const kysely = new Kysely<DB>({
	dialect
})

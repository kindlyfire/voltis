import { Sequelize } from 'sequelize'
import path from 'path'
import { newUnpackedPromise } from '../utils/utils'

function createDatabase() {
	return new Sequelize({
		dialect: 'sqlite',
		storage: path.join(process.cwd(), 'db.sqlite3'),
		// storage: ':memory:',
		logging: false
	})
}

export let db: ReturnType<typeof createDatabase>

const { promise, resolve } = newUnpackedPromise()
export const dbReady = promise

export default defineNitroPlugin(async () => {
	db = createDatabase()
	await importModel(import('../models/collection'))
	await importModel(import('../models/item'))
	await db.sync()
	resolve()
})

const importModel = async (mod: Promise<any>) => {
	;(await mod).init(db)
}

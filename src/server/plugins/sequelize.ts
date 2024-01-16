import { Sequelize } from 'sequelize'
import path from 'path'
import { newUnpackedPromise } from '../utils/utils'
import fs from 'fs-extra'

function createDatabase() {
	const runtimeConfig = useRuntimeConfig()
	return new Sequelize({
		dialect: 'sqlite',
		storage: path.join(runtimeConfig.dataDir, 'db.sqlite3'),
		// storage: ':memory:',
		logging: false
	})
}

export let db: ReturnType<typeof createDatabase>

const { promise, resolve } = newUnpackedPromise()
export const dbReady = promise

export default defineNitroPlugin(async () => {
	const runtimeConfig = useRuntimeConfig()
	await fs.mkdir(runtimeConfig.dataDir, { recursive: true })

	db = createDatabase()

	await importModel(import('../models/collection'))
	await importModel(import('../models/item'))
	await importModel(import('../models/user'))
	await importModel(import('../models/user-session'))
	await importModel(import('../models/library'))
	await import('../models/_associations').then(v => v.associate())

	await db.sync()
	resolve()
})

const importModel = async (mod: Promise<any>) => {
	;(await mod).init(db)
}

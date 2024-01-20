import bcrypt from 'bcrypt'
import { nanoid } from 'nanoid'
import { User } from '@prisma/client'

export const dbUtils = {
	createId() {
		return nanoid(14)
	},
	user: {
		async hashPassword(password: string) {
			return await bcrypt.hash(password, 10)
		},
		async checkPassword(user: User, password: string) {
			return await bcrypt.compare(password, user.password)
		}
	},
	userSession: {
		createToken() {
			return nanoid(32)
		}
	}
}

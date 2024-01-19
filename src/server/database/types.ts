import { DiskItemComicMetadata } from '../scanning/comic/metadata-file'

declare global {
	namespace PrismaJson {
		type DiskItemMetadata = {
			comic?: DiskItemComicMetadata
		}

		type CollectionMetadata = {
			comic?: {
				description: string | null
				authors: string[]
				pubYear: number | null
				pubStatus: string | null
				titles: string[]
			}
		}
	}
}

import fs from 'fs-extra'
import { MaybePromise } from '../../utils'
import { Collection } from '../models/collection'
import { Item } from '../models/item'

export interface MatcherCollection {
	contentId: string
	defaultName: string
	coverPath?: string | null
}

export interface MatcherItem {
	contentId: string
	defaultName: string
	path: string
	coverPath?: string | null
}

export interface Matcher {
	/**
	 * Returns a content ID for a given path if the files list indicates it is a
	 * collection with items that this matcher can handle. Returns a default
	 * name for the collection, and optionally a path to a cover image.
	 */
	checkIsCollection(
		dir: string,
		dirEntries: fs.Dirent[]
	): MaybePromise<MatcherCollection | null>

	/**
	 * Update all data possible for a collection, without changing either the
	 * path or the content ID. If possible, do the least I/O possible.
	 */
	updateCollection(col: Collection): MaybePromise<any>

	/**
	 * Return the content IDs of all items in a collection, along with their
	 * path. Caller will attempt reconciliation if the path of an existing item
	 * has changed.
	 */
	listItems(
		col: Collection,
		dirEntries: fs.Dirent[]
	): MaybePromise<MatcherItem[]>

	/**
	 * Update all items for a collection.
	 */
	updateItems(col: Collection, existingItems: Item[]): MaybePromise<any>
}

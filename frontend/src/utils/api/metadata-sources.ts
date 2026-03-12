import { apiFetch } from '../fetch'
import type { MangaBakaSearchResponse } from './types'

export const metadataSourcesApi = {
    searchMangaBaka: async (
        query: string,
        type: 'comic' | 'book'
    ): Promise<MangaBakaSearchResponse> => {
        const params = new URLSearchParams({ q: query, type })
        return apiFetch<MangaBakaSearchResponse>(
            `/metadata-sources/mangabaka/search?${params}`
        )
    },

    linkMangaBaka: async (contentId: string, mangabakaId: number): Promise<void> => {
        await apiFetch('/metadata-sources/mangabaka/link', {
            method: 'POST',
            body: JSON.stringify({ content_id: contentId, mangabaka_id: mangabakaId }),
        })
    },

    unlink: async (contentId: string, source: string): Promise<void> => {
        await apiFetch('/metadata-sources/unlink', {
            method: 'POST',
            body: JSON.stringify({ content_id: contentId, source }),
        })
    },
}

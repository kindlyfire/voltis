import { useQueryClient } from '@tanstack/vue-query'
import { useToast, type ToastOptions } from '@/ui/useToast'
import { linkAction, resolveReview } from './api/metadata'
import { RequestError } from './fetch'

/** A link decision, by the revision it saved. */
export interface UndoItem {
    content_id: string
    provider: string
    rev: number
}

/**
 * Toasts a link decision with an Undo that restores the links it changed, unless they changed since.
 * Call it from a `mutateAsync` continuation: a `mutate` callback is dropped if the component unmounts
 * meanwhile. Swallow the rejection there, as QueryError shows it.
 */
export function useUndoToast() {
    const client = useQueryClient()
    const toast = useToast()

    async function undo(items: UndoItem[]) {
        try {
            if (items.length === 1) {
                const { content_id, provider, rev } = items[0]!
                await linkAction(client, { action: 'undo', contentId: content_id, provider, rev })
                toast.show({ message: 'Undone' })
                return
            }
            const { results } = await resolveReview(
                client,
                items.map(i => ({
                    content_id: i.content_id,
                    provider: i.provider,
                    action: 'undo',
                    expect_rev: i.rev,
                }))
            )
            const failed = results.filter(r => !r.ok).length
            toast.show({
                message: `${results.length - failed} undone` + (failed ? `, ${failed} failed` : ''),
                tone: failed ? 'danger' : undefined,
            })
        } catch (err) {
            toast.show({
                message: `Couldn't undo: ${RequestError.getMessage(err)}`,
                tone: 'danger',
            })
        }
    }

    return (message: string, items: UndoItem[], tone?: ToastOptions['tone']) => {
        toast.show({
            message,
            tone,
            action: items.length
                ? {
                      label: 'Undo',
                      altText: 'Change the link from the series page',
                      onClick: () => void undo(items),
                  }
                : undefined,
        })
    }
}

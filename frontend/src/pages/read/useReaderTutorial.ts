import { watch, type Ref } from 'vue'
import { usersApi } from '@/utils/api/users'
import { showReaderTutorial } from './ReaderTutorialModal.vue'
import type { ReaderKind } from './shortcuts'

const FLAG = { comic: 'comicReader', book: 'bookReader' } as const

/** Shows the tutorial once per reader, then records it server-side. A failed
 * save only means it shows again next time. */
export function useReaderTutorial(kind: ReaderKind, ready: Ref<boolean>) {
    const qMe = usersApi.useMe()
    const patch = usersApi.usePatchPreferences()
    const key = FLAG[kind]
    let shown = false

    watch(
        [ready, () => qMe.data.value],
        ([isReady, me]) => {
            if (shown || !isReady || !me || me.preferences.tutorials?.[key]) return
            // Set before the await: the reader stays mounted across
            // sibling-entry navigation, which flips `ready` false→true again.
            shown = true
            void showReaderTutorial(kind).then(() => {
                patch.mutate({ tutorials: { [key]: true } })
            })
        },
        { immediate: true }
    )
}

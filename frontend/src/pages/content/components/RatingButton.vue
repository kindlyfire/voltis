<template>
    <div class="flex items-center gap-1">
        <fieldset class="rating" @pointerleave="hover = null" @keydown="onKeydown" @keyup="onKeyup">
            <legend class="sr-only">Your rating</legend>
            <label
                v-for="star in 5"
                :key="star"
                class="rating__star"
                :class="{ filled: star <= shown }"
                @pointerenter="$event.pointerType === 'mouse' && (hover = star)"
            >
                <input
                    type="radio"
                    :name="name"
                    :value="star"
                    :checked="star === selected"
                    @change="select(star, arrowKey)"
                />
                <AIcon :icon="star <= shown ? IconStarFilled : IconStar" />
                <span class="sr-only">{{ plural(star, 'star') }}</span>
            </label>
        </fieldset>
        <AIconButton
            :icon="IconClose"
            label="Clear rating"
            size="sm"
            :disabled="selected == null"
            focusable-when-disabled
            :loading="mUpdateUserData.isPending.value"
            @click="select(null)"
        />
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import { IconClose, IconStar, IconStarFilled } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { plural } from '@/utils/misc'

const props = defineProps<{
    contentId: string | null | undefined
}>()

const name = useId()
const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data
const mUpdateUserData = contentApi.useUpdateUserData()
const toast = useToast()

const currentRating = computed(() => content.value?.user_data?.rating ?? null)
/** The chosen rating until it's saved; `undefined` when nothing is waiting. */
const pending = ref<number | null>()
const selected = computed(() => (pending.value !== undefined ? pending.value : currentRating.value))
const hover = ref<number | null>(null)
const shown = computed(() => hover.value ?? selected.value ?? 0)

// Arrow keys select as they move (radio semantics): their changes save once the selection
// settles. Clicks and Space save at once.
let arrowKey = false
let timer: ReturnType<typeof setTimeout> | undefined
/** The content the pending rating is for: the route can switch content under this component. */
let pendingId: string | undefined

function onKeydown(e: KeyboardEvent) {
    hover.value = null
    arrowKey = e.key.startsWith('Arrow')
}

// An arrow press that changed nothing must not debounce the next click.
function onKeyup() {
    arrowKey = false
}

function select(rating: number | null, debounce = false) {
    pending.value = rating
    pendingId = content.value?.id
    arrowKey = false
    clearTimeout(timer)
    timer = undefined
    if (debounce) timer = setTimeout(save, 400)
    else save()
}

// Leaving the page or switching content must not drop a rating that is still waiting.
function flush() {
    if (timer === undefined) return
    clearTimeout(timer)
    save()
}
onBeforeUnmount(flush)
watch(
    () => props.contentId,
    () => {
        flush()
        pending.value = undefined
    }
)

async function save() {
    timer = undefined
    const rating = pending.value
    const contentId = pendingId
    if (rating === undefined || !contentId) return
    try {
        if (contentId !== content.value?.id || rating !== currentRating.value) {
            await mUpdateUserData.mutateAsync({ contentId, rating })
            toast.show({
                message: rating ? `Rated ${plural(rating, 'star')}` : 'Cleared your rating',
            })
        }
    } catch {
        toast.show({ message: 'Could not save your rating', tone: 'danger' })
    }
    if (pending.value === rating) pending.value = undefined
}
</script>

<style scoped>
@layer ui {
    .rating {
        display: flex;
        min-width: 0;
        margin: 0;
        padding: 0;
        border: 0;
    }

    .rating__star {
        position: relative;
        display: grid;
        place-items: center;
        width: 36px;
        height: 36px;
        border-radius: 999px;
        color: var(--color-fg-muted);
        font-size: 26px;
        cursor: pointer;

        &.filled {
            color: var(--color-star);
        }

        &:has(input:focus-visible) {
            outline: 2px solid var(--color-primary);
            outline-offset: -2px;
        }
    }

    input {
        position: absolute;
        inset: 0;
        margin: 0;
        opacity: 0;
        appearance: none;
        cursor: inherit;
    }
}
</style>

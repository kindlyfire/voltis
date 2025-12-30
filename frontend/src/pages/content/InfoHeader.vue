<template>
    <div class="relative mb-20!">
        <div class="top-background-wrapper">
            <div
                :style="{
                    backgroundImage: content?.cover_uri
                        ? `url(${API_URL}/files/cover/${content.id}?v=${content.file_mtime})`
                        : undefined,
                }"
                class="top-background"
                :class="!content.cover_uri && 'top-background--no-bg'"
            ></div>
        </div>

        <VContainer class="relative translate-y-20">
            <div class="flex gap-3 md:gap-6">
                <div class="shrink-0">
                    <VCard>
                        <div class="w-[100px] sm:w-[125px] md:w-[200px]">
                            <img
                                v-if="content?.cover_uri"
                                :src="`${API_URL}/files/cover/${content.id}`"
                                class="rounded"
                                :style="{
                                    aspectRatio: '2 / 3',
                                    objectFit: 'cover',
                                    width: '100%',
                                }"
                            />
                        </div>
                    </VCard>
                </div>
                <div class="space-y-4! grow">
                    <h1
                        class="text-xl sm:text-2xl md:text-3xl xl:text-5xl font-bold! text-shadow-md/40! text-white!"
                    >
                        {{ content?.title }}
                    </h1>
                    <div class="text-shadow-md/40! text-white!">
                        <template v-if="content?.type === 'comic_series'">Comic Series</template>
                        <template v-else-if="content?.type === 'book_series'">Book Series</template>
                    </div>
                    <dl v-if="content?.meta" class="metadata-list">
                        <template v-if="content.meta.authors?.length">
                            <dt>Author{{ content.meta.authors.length > 1 ? 's' : '' }}</dt>
                            <dd>{{ content.meta.authors.join(', ') }}</dd>
                        </template>
                        <template v-if="content.meta.publisher">
                            <dt>Publisher</dt>
                            <dd>{{ content.meta.publisher }}</dd>
                        </template>
                        <template v-if="content.meta.publication_date">
                            <dt>Published</dt>
                            <dd>{{ content.meta.publication_date }}</dd>
                        </template>
                        <template v-if="content.meta.language">
                            <dt>Language</dt>
                            <dd>{{ content.meta.language }}</dd>
                        </template>
                    </dl>
                    <div class="space-y-2!">
                        <div class="flex gap-3 flex-col sm:flex-row w-full">
                            <ReadingStatusButton :content-id="content.id" />
                            <ContinueReadingButton :content-id="content.id" />
                            <OptionsButton :content-id="content.id" />
                            <VBtn
                                icon
                                class="h-12!"
                                :loading="mUpdateUserData.isPending.value"
                                :title="isStarred ? 'Unstar' : 'Star'"
                                :color="isStarred ? 'yellow-darken-2' : undefined"
                                @click="toggleStar"
                                variant="text"
                            >
                                <VIcon :color="isStarred ? 'yellow-darken-2' : 'white'">
                                    {{ isStarred ? 'mdi-star' : 'mdi-star-outline' }}
                                </VIcon>
                            </VBtn>
                        </div>
                        <RatingButton :content-id="content.id" />
                    </div>
                </div>
            </div>
        </VContainer>
    </div>
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
import { computed } from 'vue'
import ReadingStatusButton from './components/ReadingStatusButton.vue'
import ContinueReadingButton from './components/ContinueReadingButton.vue'
import RatingButton from './components/RatingButton.vue'
import OptionsButton from './components/OptionsButton.vue'
import type { Content } from '@/utils/api/types'

const props = defineProps<{
    content: Content
}>()

const mUpdateUserData = contentApi.useUpdateUserData()
const isStarred = computed(() => props.content.user_data?.starred ?? false)

async function toggleStar() {
    mUpdateUserData.mutateAsync({
        contentId: props.content.id,
        starred: !isStarred.value,
    })
}
</script>

<style scoped>
/* Wrapper needed for the "overflow: hidden", so that the scaleX of the actual
background doesn't cause the page to widen. */
.top-background-wrapper {
    position: absolute;
    top: -30px;
    left: 0;
    right: 0;
    bottom: -30px;
    overflow: hidden;
}

.top-background {
    background-size: cover;
    background-position: center;
    filter: blur(10px) brightness(0.7);
    height: calc(100% - 30px);
    width: 100%;
    transform: scaleX(1.1);
}

.top-background--no-bg {
    background-color: #333;
}

.metadata-list {
    color: white;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
}

.metadata-list dt {
    font-size: 0.875rem;
}

.metadata-list dd {
    margin: 0;
    font-size: 0.875rem;
}

.description-text {
    white-space: pre-wrap;
    margin: 0;
}
</style>

<template>
    <!-- Mobile view -->
    <div class="md:hidden relative">
        <div v-if="content.cover_uri">
            <div
                :style="{
                    backgroundImage: content?.cover_uri
                        ? `url(${API_URL}/files/cover/${content.id}?v=${content.file_mtime})`
                        : undefined,
                }"
                class="banner-mobile"
            ></div>
            <div class="banner-mobile-overlay"></div>
        </div>

        <VContainer class="relative space-y-2!">
            <div class="flex gap-3">
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
                <div class="grow">
                    <h1 class="text-2xl md:text-5xl xl:text-5xl font-bold! text-shadow-md/10!">
                        {{ content?.title }}
                    </h1>
                    <div class="text-sm">
                        <template v-if="content?.type === 'comic_series'"> Comic Series </template>
                        <template v-else-if="content?.type === 'book_series'">
                            Book Series
                        </template>
                    </div>
                </div>
            </div>

            <div class="space-y-2!">
                <ReadingStatusButton :content-id="content.id" />
                <div class="flex gap-3 flex-row w-full">
                    <ContinueReadingButton class="grow!" :content-id="content.id" />
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
                        <VIcon :color="isStarred ? 'yellow-darken-2' : undefined">
                            {{ isStarred ? 'mdi-star' : 'mdi-star-outline' }}
                        </VIcon>
                    </VBtn>
                </div>
                <RatingButton :content-id="content.id" />
            </div>
        </VContainer>
    </div>

    <!-- Desktop view -->
    <div class="hidden md:block">
        <div class="relative">
            <div
                :style="{
                    backgroundImage: content?.cover_uri
                        ? `url(${API_URL}/files/cover/${content.id}?v=${content.file_mtime})`
                        : undefined,
                }"
                class="banner-desktop"
                :class="!content.cover_uri && 'top-background--no-bg'"
            ></div>
            <VContainer class="relative pt-30! min-h-60">
                <div class="flex gap-6">
                    <div class="w-[200px] shrink-0"></div>
                    <div class="space-y-4! grow">
                        <h1
                            class="text-xl sm:text-2xl md:text-3xl xl:text-5xl font-bold! text-shadow-md/40! text-white!"
                        >
                            {{ content?.title }}
                        </h1>
                        <div class="text-shadow-md/40! text-white!">
                            <template v-if="content?.type === 'comic_series'">
                                Comic Series
                            </template>
                            <template v-else-if="content?.type === 'book_series'">
                                Book Series
                            </template>
                        </div>
                        <dl
                            v-if="content?.meta"
                            class="metadata-list text-shadow-md/40! text-white"
                        >
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
                    </div>
                </div>
            </VContainer>
        </div>

        <VContainer class="pt-3!" :style="{ marginTop: -(300 - 84) + 'px' }">
            <div class="flex gap-6 items-end">
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
                                <VIcon :color="isStarred ? 'yellow-darken-2' : undefined">
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
.banner-desktop {
    position: absolute;
    inset: 0;
    background-size: cover;
    background-position: center;
    filter: brightness(0.7);
}

.banner-desktop-empty {
    background-color: #333;
}

.banner-mobile {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 300px;
    background-size: cover;
}

.banner-mobile-overlay {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 300px;
    background: linear-gradient(
        to bottom,
        rgba(var(--v-theme-background), 0.8),
        rgba(var(--v-theme-background), 1)
    );
}

.metadata-list {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
}

.metadata-list dt {
    font-size: 0.875rem;
    font-weight: 600;
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

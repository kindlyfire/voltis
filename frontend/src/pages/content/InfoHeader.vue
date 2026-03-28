<template>
    <!-- Mobile view -->
    <div class="relative md:hidden">
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
                        <div
                            class="w-[100px] cursor-pointer sm:w-[125px] md:w-[200px]"
                            @click="showCoverOverlay = true"
                        >
                            <img
                                v-if="content?.cover_uri"
                                :src="`${API_URL}/files/cover/${content.id}`"
                                class="block rounded"
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
                    <div v-if="parent">
                        <RouterLink :to="`/${parent.id}`" class="text-shadow-md/10!">
                            {{ parent.title }}
                        </RouterLink>
                    </div>
                    <h1 class="text-2xl font-bold! text-shadow-md/10! md:text-5xl xl:text-5xl">
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
                <div class="flex w-full flex-row gap-3">
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
            <VContainer class="relative min-h-60 pt-30!">
                <div class="flex gap-6">
                    <div class="w-[200px] shrink-0"></div>
                    <div class="grow space-y-4! text-white!">
                        <div v-if="parent">
                            <RouterLink
                                :to="`/${parent.id}`"
                                class="text-lg font-bold text-shadow-md/10!"
                            >
                                {{ parent.title }}
                            </RouterLink>
                        </div>
                        <h1
                            class="text-xl font-bold! text-shadow-md/40! sm:text-2xl md:text-3xl xl:text-5xl"
                        >
                            {{ content?.title }}
                        </h1>
                        <div class="text-shadow-md/40!">
                            <template v-if="content?.type === 'comic_series'">
                                Comic Series
                            </template>
                            <template v-else-if="content?.type === 'book_series'">
                                Book Series
                            </template>
                        </div>
                        <dl
                            v-if="content?.meta"
                            class="metadata-list text-white text-shadow-md/40!"
                        >
                            <template v-if="content.meta.staff?.length">
                                <dt>Staff</dt>
                                <dd>
                                    {{
                                        content.meta.staff
                                            .map(s => `${s.name} (${s.role})`)
                                            .join(', ')
                                    }}
                                </dd>
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
                            <template v-if="content.meta.mangabaka_id">
                                <dt>Links</dt>
                                <dd>
                                    <a
                                        :href="`https://mangabaka.org/${content.meta.mangabaka_id}`"
                                        target="_blank"
                                        class="text-inherit underline"
                                    >
                                        MangaBaka
                                    </a>
                                </dd>
                            </template>
                        </dl>
                    </div>
                </div>
            </VContainer>
        </div>

        <VContainer class="pt-3!" :style="{ marginTop: -(300 - 84) + 'px' }">
            <div class="flex items-end gap-6">
                <div class="shrink-0">
                    <VCard>
                        <div
                            class="w-[100px] cursor-pointer sm:w-[125px] md:w-[200px]"
                            @click="showCoverOverlay = true"
                        >
                            <img
                                v-if="content?.cover_uri"
                                :src="`${API_URL}/files/cover/${content.id}`"
                                class="block rounded"
                                :style="{
                                    aspectRatio: '2 / 3',
                                    objectFit: 'cover',
                                    width: '100%',
                                }"
                            />
                        </div>
                    </VCard>
                </div>
                <div class="grow space-y-4!">
                    <div class="space-y-2!">
                        <div class="flex w-full flex-col gap-3 sm:flex-row">
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

    <VOverlay
        v-model="showCoverOverlay"
        class="align-center justify-center"
        scrim
        @click="showCoverOverlay = false"
    >
        <img
            v-if="content?.cover_uri"
            :src="`${API_URL}/files/cover/${content.id}`"
            class="cover-overlay-image"
        />
    </VOverlay>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import ContinueReadingButton from './components/ContinueReadingButton.vue'
import OptionsButton from './components/OptionsButton.vue'
import RatingButton from './components/RatingButton.vue'
import ReadingStatusButton from './components/ReadingStatusButton.vue'

const props = defineProps<{
    content: Content
}>()

const qParent = contentApi.useGet(() => props.content.parent_id || undefined)
const parent = qParent.data

const mUpdateUserData = contentApi.useUpdateUserData()
const isStarred = computed(() => props.content.user_data?.starred ?? false)

const showCoverOverlay = ref(false)

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
    filter: brightness(0.6);
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

.cover-overlay-image {
    max-width: 90vw;
    max-height: 90vh;
    object-fit: contain;
}
</style>

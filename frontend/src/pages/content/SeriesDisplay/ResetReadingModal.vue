<template>
    <VDialog
        :model-value="modelValue"
        @update:model-value="
            $event => (mResetReading.isPaused.value ? null : $emit('update:modelValue', $event))
        "
        max-width="400"
    >
        <VCard>
            <VCardTitle>You've completed this series</VCardTitle>
            <VCardText>
                <div>Do you want to start again? This will mark all chapters as unread.</div>
                <AQueryError :mutation="mResetReading" class="mt-4" />
            </VCardText>

            <VCardActions>
                <VSpacer />
                <VBtn
                    variant="text"
                    @click="$emit('update:modelValue', false)"
                    :disabled="mResetReading.isPending.value"
                >
                    No
                </VBtn>
                <VBtn
                    color="primary"
                    @click="mResetReading.mutate()"
                    :loading="mResetReading.isPending.value"
                >
                    Yes
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { useMutation } from '@tanstack/vue-query'
import { useRouter } from 'vue-router'
import AQueryError from '@/components/AQueryError.vue'

const props = defineProps<{
    modelValue: boolean
    contentId: string
}>()

const emit = defineEmits<{
    'update:modelValue': [value: boolean]
}>()

const router = useRouter()
const qChildren = contentApi.useList(() => ({
    parent_id: props.contentId,
    sort: 'order',
    sort_order: 'asc',
}))

const mResetReading = useMutation({
    mutationFn: async () => {
        await contentApi.resetSeriesProgress(props.contentId)
        await contentApi.updateUserData(props.contentId, { status: 'reading' })

        const firstChild = qChildren.data.value?.data[0]
        if (firstChild) {
            router.push('/' + firstChild.id)
        }

        emit('update:modelValue', false)
    },
})
</script>

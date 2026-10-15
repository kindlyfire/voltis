<template>
    <AButton
        v-if="isAdmin"
        variant="text"
        tone="neutral"
        size="sm"
        :leading-icon="IconSync"
        :loading="mScan.isPending.value"
        @click="scan"
    >
        Scan all libraries
    </AButton>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AButton from '@/ui/AButton.vue'
import { IconSync } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { librariesApi } from '@/utils/api/libraries'
import { usersApi } from '@/utils/api/users'
import { RequestError } from '@/utils/fetch'

const qMe = usersApi.useMe()
const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))
const mScan = librariesApi.useScan()
const toast = useToast()

async function scan() {
    try {
        await mScan.mutateAsync({})
    } catch (err) {
        toast.show({
            message: `Couldn't start the scans: ${RequestError.getMessage(err)}`,
            tone: 'danger',
        })
    }
}
</script>

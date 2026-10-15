<template>
    <ADialog :open="open" title="Logs" size="lg" @update:open="v => !v && close()">
        <ASpinner v-if="loading" class="mx-auto flex" />
        <AAlert v-else-if="error" tone="danger">{{ RequestError.getMessage(error) }}</AAlert>
        <TaskLog v-else-if="text" :text="text" />
        <p v-else class="text-fg-muted py-4 text-center">No logs.</p>

        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { tasksApi } from '@/utils/api/tasks'
import { RequestError } from '@/utils/fetch'
import TaskLog from './TaskLog.vue'

const props = defineProps<{ open: boolean; close: () => void; taskId: string }>()

const loading = ref(true)
const error = ref<unknown>(null)
const text = ref('')

tasksApi
    .logs(props.taskId, 0)
    .then(res => (text.value = res.text))
    .catch(err => (error.value = err))
    .finally(() => (loading.value = false))
</script>

<script lang="ts">
import { Modals, type ShowOptions } from '@/utils/modals'
import Self from './TaskLogsModal.vue'

export function showTaskLogs(taskId: string, options?: ShowOptions): Promise<void> {
    return Modals.show<void>(Self, { taskId }, options)
}
</script>

<template>
    <ADialog v-model:open="isOpen" :title="`Edit ${draft?.def.label ?? ''}`" size="md">
        <form v-if="draft" :id="formId" @submit.prevent="confirm()">
            <div v-if="draft.def.type === 'staff'" class="flex flex-col gap-2">
                <div v-for="(entry, i) in draft.staff" :key="i" class="flex items-end gap-2">
                    <ATextField v-model="entry.name" label="Name" class="flex-1" />
                    <ASelect
                        v-model="entry.role"
                        :options="draft.def.options ?? []"
                        label="Role"
                        class="w-40"
                    />
                    <AIconButton
                        :icon="IconDelete"
                        :label="`Remove ${entry.name || 'entry'}`"
                        @click="draft.staff.splice(i, 1)"
                    />
                </div>
                <AButton
                    variant="text"
                    :leading-icon="IconPlus"
                    class="self-start"
                    @click="draft.staff.push({ name: '', role: draft.def.options?.[0] ?? '' })"
                >
                    Add a person
                </AButton>
            </div>
            <ASelect
                v-else-if="draft.def.type === 'enum'"
                v-model="draft.text"
                :options="draft.def.options ?? []"
                :label="draft.def.label"
                clearable
            />
            <ATextField
                v-else-if="draft.def.type === 'int' || draft.def.type === 'float'"
                v-model="draft.number"
                type="number"
                :label="draft.def.label"
                :error="error"
                autofocus
            />
            <ATextField
                v-else
                v-model="draft.text"
                :label="draft.def.label"
                :multiline="multiline(draft.def)"
                :auto-grow="multiline(draft.def)"
                :hint="hint(draft.def)"
                :error="error"
                autofocus
            />
        </form>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="isOpen = false">Cancel</AButton>
            <AButton type="submit" :form="formId">OK</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import AButton from '@/ui/AButton.vue'
import ADialog from '@/ui/ADialog.vue'
import AIconButton from '@/ui/AIconButton.vue'
import ASelect from '@/ui/ASelect.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconDelete, IconPlus } from '@/ui/icons'
import type { FieldDef, StaffEntry } from '@/utils/api/metadata'

const props = defineProps<{
    def: FieldDef | undefined
    /** The value the draft starts from, each time the dialog opens. */
    value: unknown
}>()
const isOpen = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ confirm: [value: unknown] }>()

const formId = useId()

interface Draft {
    def: FieldDef
    text: string | null
    number: number | null
    staff: StaffEntry[]
}

/** Kept after the dialog closes, so its content stays put while it animates out. */
const draft = ref<Draft | null>(null)

watch(isOpen, opened => {
    const def = props.def
    if (!opened || !def) return
    const value = props.value
    draft.value = {
        def,
        text: Array.isArray(value) ? value.join('\n') : value == null ? '' : String(value),
        number: typeof value === 'number' ? value : null,
        // Copied by hand, as structuredClone rejects the reactive proxies the value may hold.
        staff:
            def.type === 'staff' && Array.isArray(value)
                ? value.map(({ name, role }: StaffEntry) => ({ name, role }))
                : [],
    }
})

function multiline(def: FieldDef) {
    return def.type === 'text' || def.type === 'string_list' || def.type === 'genre_list'
}

function hint(def: FieldDef) {
    if (def.type === 'date') return 'YYYY, YYYY-MM, or YYYY-MM-DD'
    if (def.type === 'string_list' || def.type === 'genre_list') return 'One per line'
    return undefined
}

const error = computed(() => {
    const d = draft.value
    if (!d) return undefined
    const n = d.number
    if (d.def.type === 'date' && d.text && !/^\d{4}(-\d{1,2}(-\d{1,2})?)?$/.test(d.text.trim())) {
        return 'Enter YYYY, YYYY-MM, or YYYY-MM-DD'
    }
    if (n == null) return undefined
    if (d.def.type === 'int' && !Number.isInteger(n)) return 'Enter a whole number'
    if ((d.def.min != null && n < d.def.min) || (d.def.max != null && n > d.def.max)) {
        return `Enter a number from ${d.def.min ?? '…'} to ${d.def.max ?? '…'}`
    }
    return undefined
})

/** The draft's value as the server takes it; blank clears the field. */
function serialize(d: Draft): unknown {
    switch (d.def.type) {
        case 'int':
        case 'float':
            return d.number
        case 'staff': {
            const staff = d.staff.filter(s => s.name.trim())
            return staff.length ? staff : null
        }
        case 'string_list':
        case 'genre_list': {
            const items = (d.text ?? '')
                .split('\n')
                .map(s => s.trim())
                .filter(Boolean)
            return items.length ? items : null
        }
    }
    return d.text?.trim() || null
}

function confirm() {
    const d = draft.value
    if (!d || error.value) return
    isOpen.value = false
    emit('confirm', serialize(d))
}
</script>

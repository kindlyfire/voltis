import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import type { QueryOptions } from './_utils'
import type { Paginated, Task, TaskListParams } from './types'

export const tasksApi = {
    useList: (
        params: MaybeRefOrGetter<TaskListParams>,
        options: QueryOptions<Paginated<Task>> = {}
    ) =>
        useQuery({
            queryKey: computed(() => ['tasks', toValue(params)]),
            queryFn: async () => {
                const p = toValue(params)
                const searchParams = new URLSearchParams()
                if (p.limit !== undefined) searchParams.append('limit', String(p.limit))
                if (p.offset !== undefined) searchParams.append('offset', String(p.offset))
                if (p.sort) searchParams.append('sort', p.sort)
                if (p.sort_order) searchParams.append('sort_order', p.sort_order)

                const query = searchParams.toString()
                return apiFetch<Paginated<Task>>(`/tasks${query ? `?${query}` : ''}`)
            },
            ...options,
        }),
}

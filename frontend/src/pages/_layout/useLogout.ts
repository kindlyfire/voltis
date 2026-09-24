import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { authApi } from '@/utils/api/auth'
import { usersApi } from '@/utils/api/users'

export function useLogout() {
    const router = useRouter()
    const qMe = usersApi.useMe()
    const mutation = authApi.useLogout()

    const canLogout = computed(() => qMe.data.value?.can_logout !== false)

    async function logout() {
        const result = await mutation.mutateAsync()
        if (result.redirect_url) {
            window.location.href = result.redirect_url
            return
        }
        router.push('/auth/login?local=1')
    }

    return { canLogout, logout, mutation }
}

<template>
    <nav aria-label="Main" class="flex flex-col gap-0.5">
        <template v-if="isSettings">
            <ANavItem to="/" exact :icon="IconArrowLeft" label="Back" />
            <ADivider class="mx-3.5 my-2.5" />
            <ANavItem
                to="/settings/interface"
                :icon="IconMonitor"
                :active-icon="IconMonitorFilled"
                label="Interface"
            />
            <ANavItem
                to="/settings/account"
                :icon="IconAccount"
                :active-icon="IconAccountFilled"
                label="Account"
            />
            <ANavItem to="/settings/opds" :icon="IconRss" label="OPDS" />
            <ANavItem
                v-if="hasBrokenRefs || route.path === '/settings/broken-refs'"
                to="/settings/broken-refs"
                :icon="IconLinkOff"
                label="Broken Refs"
            />
            <template v-if="isAdmin">
                <ADivider class="mx-3.5 my-2.5" />
                <ANavItem to="/settings/general" :icon="IconTune" label="General" />
                <ANavItem
                    to="/settings/users"
                    :icon="IconAccountGroup"
                    :active-icon="IconAccountGroupFilled"
                    label="Users"
                />
                <ANavItem
                    to="/settings/libraries"
                    :icon="IconBookshelf"
                    :active-icon="IconBookshelfFilled"
                    label="Libraries"
                />
                <ANavItem
                    to="/settings/metadata"
                    :icon="IconLink"
                    label="Metadata"
                    :badge="reviewCount"
                />
                <ANavItem
                    to="/settings/tasks"
                    :icon="IconClipboardList"
                    :active-icon="IconClipboardListFilled"
                    label="Tasks"
                />
            </template>
            <ADivider class="mx-3.5 my-2.5" />
            <ANavItem href="https://voltis.tijlvdb.me/" :icon="IconOpenInNew" label="Docs" />
            <p v-if="qInfo.data.value" class="text-fg-muted mt-2 text-center text-xs">
                Voltis {{ qInfo.data.value.version }}
            </p>
        </template>
        <template v-else>
            <ANavItem to="/" exact :icon="IconHome" :active-icon="IconHomeFilled" label="Home" />
            <ANavItem
                to="/lists"
                exact
                :icon="IconList"
                :active-icon="IconListFilled"
                label="Lists"
            />
            <Libraries />
            <ADivider class="mx-3.5 my-2.5" />
            <ANavItem
                to="/settings/interface"
                :icon="IconCog"
                :active-icon="IconCogFilled"
                label="Settings"
            />
        </template>
    </nav>

    <div class="border-outline-variant mt-auto flex items-center gap-1 border-t pt-3 pb-0.5 pl-1.5">
        <AMenu v-if="canLogout" side="top">
            <template #trigger>
                <button type="button" class="user-button a-state a-focus">
                    <AAvatar :name="username" />
                    <span class="truncate">{{ username }}</span>
                </button>
            </template>
            <AMenuItem
                :leading-icon="IconLogout"
                :disabled="mLogout.isPending.value"
                @select="logout"
            >
                Logout
            </AMenuItem>
        </AMenu>
        <div v-else class="user-button">
            <AAvatar :name="username" />
            <span class="truncate">{{ username }}</span>
        </div>
        <ATooltip text="Toggle theme (Shift+click or long press: follow the system)" side="top">
            <AIconButton
                :icon="store.theme === 'dark' ? IconWeatherSunny : IconWeatherNight"
                label="Toggle theme"
                :tooltip="false"
                @pointerdown="startPress"
                @pointerup="cancelPress"
                @pointerleave="cancelPress"
                @pointercancel="cancelPress"
                @contextmenu.prevent
                @click="onThemeClick"
            />
        </ATooltip>
    </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import AAvatar from '@/ui/AAvatar.vue'
import ADivider from '@/ui/ADivider.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import ANavItem from '@/ui/ANavItem.vue'
import ATooltip from '@/ui/ATooltip.vue'
import {
    IconAccount,
    IconAccountFilled,
    IconAccountGroup,
    IconAccountGroupFilled,
    IconArrowLeft,
    IconBookshelf,
    IconBookshelfFilled,
    IconClipboardList,
    IconClipboardListFilled,
    IconCog,
    IconCogFilled,
    IconHome,
    IconHomeFilled,
    IconLink,
    IconLinkOff,
    IconList,
    IconListFilled,
    IconLogout,
    IconMonitor,
    IconMonitorFilled,
    IconOpenInNew,
    IconRss,
    IconTune,
    IconWeatherNight,
    IconWeatherSunny,
} from '@/ui/icons'
import { contentApi } from '@/utils/api/content'
import { metadataApi } from '@/utils/api/metadata'
import { miscApi } from '@/utils/api/misc'
import { usersApi } from '@/utils/api/users'
import Libraries from './Libraries.vue'
import { useLayoutStore } from './useLayoutStore'
import { useLogout } from './useLogout'

/** The sidebar's nav and user row, shared by the persistent sidebar and the nav drawer. */
const store = useLayoutStore()
const route = useRoute()
const isSettings = computed(() => route.path.startsWith('/settings'))
const qMe = usersApi.useMe()
const qInfo = miscApi.useInfo(() => isSettings.value)
const username = computed(() => qMe.data.value?.username ?? '')
const isAdmin = computed(() => qMe.data.value?.permissions.includes('ADMIN'))
const { canLogout, logout, mutation: mLogout } = useLogout()

const qBrokenRefs = contentApi.useBrokenRefsSummary({ enabled: () => isSettings.value })
// Refs whose library was deleted can't be fixed on the page, which leaves them out.
const hasBrokenRefs = computed(() => !!qBrokenRefs.data.value?.some(s => s.library_id))

const qSummary = metadataApi.useSummary({ enabled: () => isSettings.value && !!isAdmin.value })
const reviewCount = computed(() => qSummary.data.value?.libraries.reduce((n, s) => n + s.review, 0))

// Theme: click toggles, Shift+click or a long press goes back to the system theme.
let pressTimer: ReturnType<typeof setTimeout> | undefined
let longPressed = false
function startPress() {
    longPressed = false
    pressTimer = setTimeout(() => {
        longPressed = true
        store.resetTheme()
    }, 500)
}
function cancelPress() {
    clearTimeout(pressTimer)
}
function onThemeClick(e: MouseEvent) {
    // The click that ends a long press. Keyboard clicks (detail 0) never are: a long press that
    // ended off the button leaves the flag set until the next pointerdown.
    const endsLongPress = longPressed && e.detail !== 0
    longPressed = false
    if (endsLongPress) return
    if (e.shiftKey) store.resetTheme()
    else store.toggleTheme()
}
onUnmounted(cancelPress)
</script>

<style scoped>
@layer ui {
    .user-button {
        display: flex;
        flex: 1;
        align-items: center;
        gap: 12px;
        min-width: 0;
        min-height: 48px;
        padding: 0 8px;
        border: 0;
        border-radius: 12px;
        background: none;
        color: var(--color-fg);
        font-size: 14px;
        font-weight: 600;
        text-align: start;
    }

    button.user-button {
        cursor: pointer;
    }
}
</style>

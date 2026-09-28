<template>
    <div class="min-h-screen">
        <header
            class="bg-bg/90 border-outline-variant sticky top-0 z-10 flex flex-wrap items-center gap-x-6 gap-y-2 border-b px-4 py-3 backdrop-blur md:px-10"
        >
            <h1 class="font-display text-2xl font-semibold">Voltis kit</h1>
            <nav
                aria-label="Sections"
                class="order-last flex w-full gap-1 overflow-x-auto md:order-none md:w-auto md:flex-1"
            >
                <a
                    v-for="section in SECTIONS"
                    :key="section.id"
                    :href="`#${section.id}`"
                    class="text-fg-muted hover:bg-surface-3 hover:text-fg rounded-full px-3 py-1.5 text-sm font-medium whitespace-nowrap"
                >
                    {{ section.title }}
                </a>
            </nav>
            <ASegmented
                class="ml-auto"
                :model-value="layout.theme"
                :options="THEMES"
                label="Theme"
                size="sm"
                @update:model-value="theme => theme !== layout.theme && layout.toggleTheme()"
            />
        </header>

        <main class="mx-auto flex max-w-6xl flex-col gap-20 px-4 pt-8 pb-32 md:px-10">
            <p class="text-fg-muted max-w-2xl text-sm">
                Every kit component in every state. Hover and pressed states are live: point at
                things. Tab through the page to see focus rings. Dev only, at <code>/_kit</code>.
            </p>

            <!-- Foundations -->
            <KitSection
                id="foundations"
                title="Foundations"
                description="Tokens from src/ui/tokens.css."
            >
                <KitGroup title="Colors" :min="170">
                    <div v-for="token in COLOR_TOKENS" :key="token" class="flex items-center gap-3">
                        <span
                            class="border-outline-variant size-10 flex-none rounded-xl border"
                            :style="{ background: `var(--color-${token})` }"
                        />
                        <code class="text-xs">{{ token }}</code>
                    </div>
                </KitGroup>
                <KitGroup title="Type" :min="400">
                    <KitDemo label="display 40 (page title)" wide>
                        <span class="font-display text-[40px] leading-tight font-semibold"
                            >Library</span
                        >
                    </KitDemo>
                    <KitDemo label="display 30 (home section)" wide>
                        <span class="font-display text-[30px] font-semibold">Recently read</span>
                    </KitDemo>
                    <KitDemo label="display 22 / 20 (dialog / card title)" wide>
                        <span class="font-display text-[22px] font-semibold">Edit metadata</span>
                        <span class="font-display text-xl font-semibold">User details</span>
                    </KitDemo>
                    <KitDemo label="body 15 / 14 / 13 / 12" wide>
                        <span class="text-[15px]">Body 15</span>
                        <span class="text-sm">Body 14</span>
                        <span class="text-[13px]">Body 13</span>
                        <span class="text-fg-muted text-xs">Muted 12</span>
                    </KitDemo>
                </KitGroup>
                <KitGroup
                    title="Icons"
                    description="src/ui/icons.ts: the mdi names mapped to Material Symbols Light. Review the mapping here."
                    :min="170"
                >
                    <div v-for="[name, icon] in ICONS" :key="name" class="flex items-center gap-3">
                        <AIcon :icon="icon" :size="24" />
                        <code class="text-xs break-all">{{ name.replace(/^Icon/, '') }}</code>
                    </div>
                </KitGroup>
            </KitSection>

            <!-- Actions -->
            <KitSection id="actions" title="Actions">
                <KitGroup title="AButton" :min="300">
                    <KitDemo
                        v-for="variant in BUTTON_VARIANTS"
                        :key="variant"
                        :label="variant"
                        stack
                    >
                        <div class="flex flex-wrap gap-2">
                            <AButton :variant="variant">Primary</AButton>
                            <AButton :variant="variant" tone="neutral">Neutral</AButton>
                            <AButton :variant="variant" tone="danger">Danger</AButton>
                        </div>
                        <div class="flex flex-wrap gap-2">
                            <AButton :variant="variant" :leading-icon="IconPlus">Icon</AButton>
                            <AButton :variant="variant" disabled>Disabled</AButton>
                            <AButton :variant="variant" loading>Loading</AButton>
                            <AButton :variant="variant" :leading-icon="IconSync" loading>
                                Syncing
                            </AButton>
                        </div>
                    </KitDemo>
                    <KitDemo label="sizes sm / md / lg">
                        <AButton size="sm">Small</AButton>
                        <AButton>Medium</AButton>
                        <AButton size="lg">Save settings</AButton>
                    </KitDemo>
                    <KitDemo label="links: to, href (external), disabled link">
                        <AButton variant="text" to="/_kit#actions">Router link</AButton>
                        <AButton variant="text" href="https://example.com">External</AButton>
                        <AButton variant="text" to="/" disabled>Disabled link</AButton>
                    </KitDemo>
                    <KitDemo label="trailing icon, block" stack>
                        <AButton variant="tonal" :trailing-icon="IconChevronDown">Options</AButton>
                        <AButton block>Block</AButton>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="AIconButton" :min="260">
                    <KitDemo
                        v-for="variant in ICON_BUTTON_VARIANTS"
                        :key="variant"
                        :label="variant"
                    >
                        <AIconButton :variant="variant" :icon="IconPencil" label="Edit" />
                        <AIconButton
                            :variant="variant"
                            tone="primary"
                            :icon="IconStar"
                            label="Favorite"
                        />
                        <AIconButton
                            :variant="variant"
                            tone="danger"
                            :icon="IconDelete"
                            label="Delete"
                        />
                        <AIconButton
                            :variant="variant"
                            :icon="IconPencil"
                            label="Edit (disabled)"
                            disabled
                        />
                        <AIconButton :variant="variant" :icon="IconSync" label="Sync" loading />
                    </KitDemo>
                    <KitDemo label="sizes sm / md / lg">
                        <AIconButton size="sm" :icon="IconTune" label="Display options" />
                        <AIconButton :icon="IconTune" label="Display options" />
                        <AIconButton size="lg" :icon="IconTune" label="Display options" />
                    </KitDemo>
                    <KitDemo label="toggle (pressed / unpressed)">
                        <AIconButton
                            :icon="IconStar"
                            :pressed-icon="IconStarFilled"
                            label="Favorites only"
                            :pressed="favorites"
                            @click="favorites = !favorites"
                        />
                        <AIconButton
                            :icon="IconSelectMode"
                            :pressed-icon="IconSelectModeFilled"
                            label="Select mode"
                            :pressed="true"
                        />
                    </KitDemo>
                    <KitDemo label="no tooltip, link">
                        <AIconButton :icon="IconClose" label="Close" :tooltip="false" />
                        <AIconButton :icon="IconHome" label="Home" to="/" />
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ASegmented" :min="300">
                    <KitDemo label="md" stack>
                        <ASegmented v-model="visibility" :options="VISIBILITY" label="Visibility" />
                    </KitDemo>
                    <KitDemo label="sm" stack>
                        <ASegmented
                            v-model="countMode"
                            :options="COUNT_MODES"
                            label="Item count"
                            size="sm"
                        />
                    </KitDemo>
                    <KitDemo label="block" stack>
                        <ASegmented
                            v-model="readerMode"
                            :options="READER_MODES"
                            label="Mode"
                            block
                        />
                    </KitDemo>
                    <KitDemo label="disabled, disabled option" stack>
                        <ASegmented
                            v-model="visibility"
                            :options="VISIBILITY"
                            label="Visibility"
                            disabled
                        />
                        <ASegmented
                            v-model="visibility"
                            :options="[
                                ...VISIBILITY.slice(0, 2),
                                { value: 'hide', label: 'Hide', disabled: true },
                            ]"
                            label="Visibility"
                        />
                    </KitDemo>
                    <KitDemo label="empty (value not in options)" stack>
                        <ASegmented
                            :model-value="noValue"
                            :options="VISIBILITY"
                            label="Visibility"
                        />
                    </KitDemo>
                </KitGroup>

                <KitGroup
                    title="ATabs"
                    description="Panels stay mounted and scroll on their own below the tab bar."
                    :min="300"
                >
                    <KitDemo label="in a bounded column" stack>
                        <div
                            class="rounded-card border-outline-variant flex h-64 flex-col overflow-hidden border"
                        >
                            <ATabs v-model="kitTab" :options="KIT_TABS" label="Sections">
                                <template #contents>
                                    <ul class="p-4 text-sm">
                                        <li v-for="c in CHAPTERS" :key="c.value" class="py-1">
                                            {{ c.label }}
                                        </li>
                                    </ul>
                                </template>
                                <template #settings>
                                    <div class="p-4">
                                        <ASegmented
                                            v-model="readerMode"
                                            :options="READER_MODES"
                                            label="Mode"
                                            block
                                        />
                                    </div>
                                </template>
                                <template #about>
                                    <p class="text-fg-muted p-4 text-sm">Nothing here.</p>
                                </template>
                                <template #tab-about>
                                    About
                                    <AIcon :icon="IconAlert" label="Empty" class="text-warning" />
                                </template>
                            </ATabs>
                        </div>
                    </KitDemo>
                </KitGroup>

                <KitGroup
                    title="usePressAndHold"
                    description="Hold a button (pointer, Enter or Space) to repeat. A short press steps once."
                >
                    <KitDemo label="columns">
                        <AIconButton
                            variant="outlined"
                            size="sm"
                            :icon="IconMinus"
                            label="Fewer columns"
                            :disabled="columns <= 1"
                            focusable-when-disabled
                            v-bind="minusHold"
                        />
                        <span class="w-8 text-center tabular-nums">{{ columns }}</span>
                        <AIconButton
                            variant="outlined"
                            size="sm"
                            :icon="IconPlus"
                            label="More columns"
                            :disabled="columns >= 20"
                            focusable-when-disabled
                            v-bind="plusHold"
                        />
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Inputs -->
            <KitSection id="inputs" title="Text inputs">
                <KitGroup title="ATextField">
                    <KitDemo label="rest" stack>
                        <ATextField v-model="text" label="Username" />
                    </KitDemo>
                    <KitDemo label="empty + placeholder" stack>
                        <ATextField v-model="empty" label="Email" placeholder="you@example.com" />
                    </KitDemo>
                    <KitDemo label="hint" stack>
                        <ATextField
                            v-model="text"
                            label="Display name"
                            hint="Shown to other users"
                        />
                    </KitDemo>
                    <KitDemo label="error" stack>
                        <ATextField
                            v-model="empty"
                            label="Password"
                            type="password"
                            error="Required"
                        />
                    </KitDemo>
                    <KitDemo label="disabled" stack>
                        <ATextField v-model="text" label="Username" disabled />
                    </KitDemo>
                    <KitDemo label="readonly (focusable, copyable)" stack>
                        <ATextField
                            model-value="https://voltis.example/oidc/callback"
                            label="Callback URL"
                            readonly
                        >
                            <template #trailing>
                                <AIconButton :icon="IconContentCopy" label="Copy" size="sm" />
                            </template>
                        </ATextField>
                    </KitDemo>
                    <KitDemo label="loading" stack>
                        <ATextField v-model="text" label="Checking…" loading />
                    </KitDemo>
                    <KitDemo label="clearable + leading icon" stack>
                        <ATextField
                            v-model="search"
                            label="Search"
                            :leading-icon="IconMagnify"
                            clearable
                        />
                    </KitDemo>
                    <KitDemo label="number + suffix" stack>
                        <ATextField
                            v-model="days"
                            label="Keep history"
                            type="number"
                            suffix="days"
                        />
                    </KitDemo>
                    <KitDemo label="sm (label as placeholder)" stack>
                        <ATextField v-model="empty" label="Filter" size="sm" />
                    </KitDemo>
                    <KitDemo label="multiline, auto-grow" stack>
                        <ATextField v-model="notes" label="Notes" multiline auto-grow :rows="2" />
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Selection -->
            <KitSection id="selection" title="Selection controls">
                <KitGroup title="ASelect" description="Esc closes only the open list.">
                    <KitDemo label="selected" stack>
                        <ASelect v-model="status" :options="STATUS" label="Status" />
                    </KitDemo>
                    <KitDemo label="empty + placeholder, clearable" stack>
                        <ASelect
                            v-model="statusEmpty"
                            :options="STATUS"
                            label="Status"
                            placeholder="Set status"
                            clearable
                        />
                    </KitDemo>
                    <KitDemo label="clearable (selected)" stack>
                        <ASelect v-model="status" :options="STATUS" label="Status" clearable />
                    </KitDemo>
                    <KitDemo label="hint" stack>
                        <ASelect
                            v-model="chapter"
                            :options="CHAPTERS"
                            label="Select chapter"
                            hint="Everything up to it is marked read."
                        />
                    </KitDemo>
                    <KitDemo label="error" stack>
                        <ASelect
                            v-model="statusEmpty"
                            :options="STATUS"
                            label="Status"
                            error="Pick a status"
                        />
                    </KitDemo>
                    <KitDemo label="disabled" stack>
                        <ASelect v-model="status" :options="STATUS" label="Status" disabled />
                    </KitDemo>
                    <KitDemo label="readonly" stack>
                        <ASelect
                            v-model="status"
                            :options="STATUS"
                            label="Status"
                            readonly
                            clearable
                        />
                    </KitDemo>
                    <KitDemo label="loading" stack>
                        <ASelect v-model="status" :options="STATUS" label="Status" loading />
                    </KitDemo>
                    <KitDemo label="sm, bare values" stack>
                        <ASelect
                            v-model="provider"
                            :options="['oidc', 'proxy']"
                            label="Provider"
                            size="sm"
                        />
                    </KitDemo>
                    <KitDemo label="#option slot" stack>
                        <ASelect v-model="chapter" :options="CHAPTERS" label="Chapter">
                            <template #option="{ option }">
                                <span class="text-fg-muted mr-2 tabular-nums"
                                    >#{{ option.value.slice(1) }}</span
                                >{{ option.label }}
                            </template>
                        </ASelect>
                    </KitDemo>
                </KitGroup>

                <KitGroup
                    title="ACombobox"
                    description="Type to filter. Above 50 options the list is virtualized, and options are one line."
                >
                    <KitDemo label="empty" stack>
                        <ACombobox v-model="uri" :options="URIS" label="New ref" clearable />
                    </KitDemo>
                    <KitDemo label="selected" stack>
                        <ACombobox v-model="uriSet" :options="URIS" label="New ref" clearable />
                    </KitDemo>
                    <KitDemo label="sm, error" stack>
                        <ACombobox
                            v-model="uri"
                            :options="URIS"
                            label="New ref"
                            size="sm"
                            error="No match"
                        />
                    </KitDemo>
                    <KitDemo label="disabled" stack>
                        <ACombobox v-model="uriSet" :options="URIS" label="New ref" disabled />
                    </KitDemo>
                    <KitDemo label="readonly" stack>
                        <ACombobox v-model="uriSet" :options="URIS" label="New ref" readonly />
                    </KitDemo>
                    <KitDemo label="virtualized, 500 options" stack>
                        <ACombobox v-model="manyChapter" :options="MANY_CHAPTERS" label="Chapter" />
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ACheckbox">
                    <KitDemo label="unchecked / checked" stack>
                        <ACheckbox v-model="check1" label="Hide item count" />
                        <ACheckbox v-model="check2" label="Hide reading status" />
                    </KitDemo>
                    <KitDemo label="indeterminate" stack>
                        <ACheckbox :model-value="false" indeterminate label="Select all" />
                    </KitDemo>
                    <KitDemo label="disabled" stack>
                        <ACheckbox :model-value="false" label="Force scan" disabled />
                        <ACheckbox :model-value="true" label="Force scan" disabled />
                    </KitDemo>
                    <KitDemo label="hint, error" stack>
                        <ACheckbox
                            v-model="check1"
                            label="Force scan"
                            hint="Re-scans every file, even unchanged ones."
                        />
                        <ACheckbox v-model="check1" label="I understand" error="Required" />
                    </KitDemo>
                    <KitDemo label="readonly" stack>
                        <ACheckbox :model-value="true" label="Admin" readonly />
                    </KitDemo>
                    <KitDemo label="size sm (toolbar, beside sm fields)" stack>
                        <ACheckbox v-model="check1" label="Errors only" size="sm" />
                    </KitDemo>
                    <KitDemo label="hidden label (grid overlay), shift-click passthrough">
                        <ACheckbox
                            :model-value="check3"
                            label="Select One Piece"
                            hide-label
                            @click="
                                e => {
                                    check3 = !check3
                                    lastShift = e.shiftKey
                                }
                            "
                        />
                        <span class="text-fg-muted text-xs">shift: {{ lastShift }}</span>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ASwitch" :min="320">
                    <KitDemo label="off / on" stack>
                        <ASwitch v-model="switch1" label="Show tutorials" />
                        <ASwitch v-model="switch2" label="Enable SSO" />
                    </KitDemo>
                    <KitDemo label="description" stack>
                        <ASwitch
                            v-model="switch2"
                            label="Match by email"
                            description="Link accounts whose verified email matches a local user."
                        />
                    </KitDemo>
                    <KitDemo label="disabled" stack>
                        <ASwitch :model-value="false" label="Allow registration" disabled />
                        <ASwitch :model-value="true" label="Allow registration" disabled />
                    </KitDemo>
                    <KitDemo label="readonly, error" stack>
                        <ASwitch :model-value="true" label="Managed by the server" readonly />
                        <ASwitch v-model="switch1" label="Accept terms" error="Required" />
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ARadioGroup" :min="300">
                    <KitDemo label="rest" stack>
                        <ARadioGroup v-model="action" :options="ACTIONS" label="Action" />
                    </KitDemo>
                    <KitDemo label="descriptions, disabled option" stack>
                        <ARadioGroup v-model="action" :options="ACTIONS_DESCRIBED" label="Action" />
                    </KitDemo>
                    <KitDemo label="disabled group, error" stack>
                        <ARadioGroup v-model="action" :options="ACTIONS" label="Action" disabled />
                        <ARadioGroup
                            :model-value="null"
                            :options="ACTIONS"
                            label="Action"
                            error="Pick one"
                        />
                    </KitDemo>
                    <KitDemo label="readonly" stack>
                        <ARadioGroup v-model="action" :options="ACTIONS" label="Action" readonly />
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ASlider" :min="280">
                    <KitDemo label="show value, formatted (aria-valuetext)" stack>
                        <ASlider
                            v-model="page"
                            label="Page"
                            show-value
                            :min="0"
                            :max="41"
                            :format-value="p => `${p + 1} of 42`"
                            @commit="v => (committed = v)"
                        />
                        <span class="text-fg-muted text-xs"
                            >last commit: {{ committed ?? '—' }}</span
                        >
                    </KitDemo>
                    <KitDemo label="steps" stack>
                        <ASlider
                            v-model="width"
                            label="Width"
                            show-value
                            :min="10"
                            :max="100"
                            :step="5"
                            :format-value="w => `${w}%`"
                        />
                    </KitDemo>
                    <KitDemo label="hidden label" stack>
                        <ASlider
                            v-model="width"
                            label="Width"
                            hide-label
                            :min="10"
                            :max="100"
                            :step="5"
                        />
                    </KitDemo>
                    <KitDemo label="disabled" stack>
                        <ASlider
                            v-model="width"
                            label="Width"
                            show-value
                            disabled
                            :min="10"
                            :max="100"
                        />
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Feedback -->
            <KitSection id="feedback" title="Feedback">
                <KitGroup title="AAlert" :min="340">
                    <KitDemo v-for="tone in ALERT_TONES" :key="tone" :label="tone" stack>
                        <AAlert :tone="tone"
                            >Something to know about, in the {{ tone }} tone.</AAlert
                        >
                    </KitDemo>
                    <KitDemo label="title + dismissible" stack>
                        <AAlert
                            v-if="!alertDismissed"
                            tone="warning"
                            title="Email matching"
                            dismissible
                            @dismiss="alertDismissed = true"
                        >
                            Anyone who registers a matching email with the provider can claim the
                            account.
                        </AAlert>
                        <AButton v-else variant="text" @click="alertDismissed = false"
                            >Show again</AButton
                        >
                    </KitDemo>
                </KitGroup>

                <KitGroup title="AProgressBar" :min="280">
                    <KitDemo label="0 / 0.35 / 1" stack>
                        <AProgressBar :value="0" label="Progress" />
                        <AProgressBar :value="0.35" label="Progress" />
                        <AProgressBar :value="1" label="Progress" />
                    </KitDemo>
                    <KitDemo label="indeterminate, tones, thickness 6" stack>
                        <AProgressBar indeterminate label="Scanning" />
                        <AProgressBar :value="1" tone="success" :thickness="6" label="Scan" />
                        <AProgressBar :value="0.6" tone="danger" :thickness="6" label="Scan" />
                    </KitDemo>
                    <KitDemo label="overlay (cover)" stack>
                        <div
                            class="relative h-24 overflow-hidden rounded-xl"
                            :style="{ background: coverGradient(200) }"
                        >
                            <AProgressBar
                                class="absolute inset-x-0 bottom-0"
                                variant="overlay"
                                :value="0.42"
                                label="Progress"
                                value-text="Chapter 5 of 12"
                            />
                        </div>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ASpinner, ASkeleton" :min="260">
                    <KitDemo label="spinner sm / md / lg">
                        <ASpinner size="sm" />
                        <ASpinner />
                        <ASpinner size="lg" />
                    </KitDemo>
                    <KitDemo label="skeleton rect / circle" stack>
                        <div class="flex items-center gap-3">
                            <ASkeleton width="96px" height="64px" />
                            <ASkeleton width="48px" shape="circle" />
                        </div>
                    </KitDemo>
                    <KitDemo label="skeleton text, 3 lines" stack>
                        <ASkeleton shape="text" :lines="3" />
                    </KitDemo>
                </KitGroup>

                <KitGroup title="AChip" :min="300">
                    <KitDemo label="tonal" wide>
                        <AChip v-for="tone in CHIP_TONES" :key="tone" :tone="tone">{{
                            tone
                        }}</AChip>
                    </KitDemo>
                    <KitDemo label="outlined" wide>
                        <AChip
                            v-for="tone in CHIP_TONES"
                            :key="tone"
                            :tone="tone"
                            variant="outlined"
                            >{{ tone }}</AChip
                        >
                    </KitDemo>
                    <KitDemo label="sm, leading icon">
                        <AChip size="sm">#12</AChip>
                        <AChip size="sm" tone="primary">Overrides</AChip>
                        <AChip :leading-icon="IconBookOpen">Comic</AChip>
                    </KitDemo>
                    <KitDemo label="button (click), link (to)">
                        <AChip :tone="chipOn ? 'primary' : 'neutral'" @click="chipOn = !chipOn"
                            >MangaBaka</AChip
                        >
                        <AChip to="/_kit#feedback" :leading-icon="IconBookshelf">Library</AChip>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ABadge, AAvatar" :min="200">
                    <KitDemo label="count, icon dot">
                        <ABadge>12</ABadge>
                        <ABadge label="3 unread">3</ABadge>
                        <ABadge :icon="IconBookOpenFilled" label="Reading" />
                    </KitDemo>
                    <KitDemo label="avatar 34 / 48">
                        <AAvatar name="tijl" />
                        <AAvatar name="Claude" :size="48" />
                    </KitDemo>
                </KitGroup>

                <KitGroup
                    title="Toasts (useToast)"
                    description="Up to 3 at once, newest at the bottom; further toasts wait (at most 3). Hover or focus pauses them all; F8 focuses the region."
                >
                    <KitDemo label="tones" wide>
                        <AButton variant="tonal" @click="toast.show({ message: 'Settings saved' })"
                            >Success</AButton
                        >
                        <AButton
                            variant="tonal"
                            @click="
                                toast.show({ message: 'Couldn\'t save settings', tone: 'danger' })
                            "
                            >Error</AButton
                        >
                        <AButton
                            variant="tonal"
                            @click="toast.show({ message: 'Copied to clipboard', tone: 'info' })"
                            >Info</AButton
                        >
                        <AButton
                            variant="tonal"
                            @click="
                                toast.show({
                                    message: 'Removed from list',
                                    action: {
                                        label: 'Undo',
                                        altText: 'Re-add it from the content page',
                                        onClick: () => toast.show({ message: 'Restored' }),
                                    },
                                })
                            "
                        >
                            With action
                        </AButton>
                        <AButton
                            variant="text"
                            @click="
                                ['First', 'Second', 'Third', 'Fourth', 'Fifth'].forEach(m =>
                                    toast.show({ message: `${m} toast` })
                                )
                            "
                        >
                            Show five
                        </AButton>
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Overlays -->
            <KitSection id="overlays" title="Overlays">
                <KitGroup title="ATooltip, AMenu, APopover" :min="260">
                    <KitDemo label="tooltip">
                        <ATooltip text="Search">
                            <AButton variant="outlined" tone="neutral">Hover me</AButton>
                        </ATooltip>
                    </KitDemo>
                    <KitDemo label="menu">
                        <AMenu>
                            <template #trigger>
                                <AIconButton :icon="IconDotsVertical" label="More actions" />
                            </template>
                            <AMenuItem
                                :leading-icon="IconPencil"
                                @select="toast.show({ message: 'Edit' })"
                                >Edit metadata</AMenuItem
                            >
                            <AMenuItem :leading-icon="IconDownload" disabled>Download</AMenuItem>
                            <AMenuItem :leading-icon="IconHome" to="/">
                                Go home
                                <template #trailing>Link</template>
                            </AMenuItem>
                            <AMenuSeparator />
                            <AMenuItem :leading-icon="IconDelete" tone="danger">Delete</AMenuItem>
                        </AMenu>
                    </KitDemo>
                    <KitDemo label="popover (stays open)">
                        <APopover v-model:open="popoverOpen" label="Display options" :width="260">
                            <template #trigger>
                                <AIconButton
                                    :icon="IconTune"
                                    label="Display options"
                                    :pressed="popoverOpen"
                                />
                            </template>
                            <div class="flex flex-col gap-1">
                                <div class="text-fg-muted text-xs">Visibility</div>
                                <ACheckbox v-model="check1" label="Hide item count" />
                                <ACheckbox v-model="check2" label="Hide reading status" />
                                <div class="text-fg-muted mt-2 mb-1 text-xs">Item count</div>
                                <ASegmented
                                    v-model="countMode"
                                    :options="COUNT_MODES"
                                    label="Item count"
                                    size="sm"
                                />
                            </div>
                        </APopover>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ADialog" :min="200">
                    <KitDemo label="md, with actions">
                        <AButton variant="tonal" @click="dialogOpen = true">Open dialog</AButton>
                    </KitDemo>
                    <KitDemo label="sm, not dismissible">
                        <AButton variant="tonal" @click="strictOpen = true">Open strict</AButton>
                    </KitDemo>
                    <KitDemo label="bare (lightbox)">
                        <AButton variant="tonal" @click="bareOpen = true">Open lightbox</AButton>
                    </KitDemo>
                </KitGroup>

                <KitGroup
                    title="ADrawer"
                    description="Not modal: no focus trap. At most 85vw wide. Esc, the scrim or a swipe toward its edge closes it."
                    :min="200"
                >
                    <KitDemo label="left / right">
                        <AButton variant="tonal" @click="drawerLeft = true">Left drawer</AButton>
                        <AButton variant="tonal" @click="drawerRight = true">Right drawer</AButton>
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Structure -->
            <KitSection id="structure" title="Structure and navigation">
                <KitGroup title="ACard" :min="320">
                    <KitDemo label="raised, title + actions" stack>
                        <ACard title="User details">
                            <p class="text-fg-muted text-sm">Your username and email.</p>
                            <template #actions><AButton>Save</AButton></template>
                        </ACard>
                    </KitDemo>
                    <KitDemo label="tonal, header actions, padding md" stack>
                        <ACard title="Linked accounts" variant="tonal" padding="md">
                            <template #headerActions>
                                <AIconButton :icon="IconPlus" label="Link account" size="sm" />
                            </template>
                            <p class="text-fg-muted text-sm">No external account is linked.</p>
                        </ACard>
                    </KitDemo>
                    <KitDemo label="stretched link (to) with inner button" stack>
                        <ACard title="Favourites" to="/_kit#structure" padding="md">
                            <p class="text-fg-muted text-sm">
                                The whole card is a link; the button still works.
                            </p>
                            <div>
                                <AButton
                                    size="sm"
                                    variant="tonal"
                                    @click="toast.show({ message: 'Inner button' })"
                                    >Inner button</AButton
                                >
                            </div>
                        </ACard>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="APageHeader" :min="400">
                    <KitDemo label="title + actions + meta" wide stack>
                        <APageHeader title="Manga">
                            <template #actions>
                                <AIconButton :icon="IconTune" label="Display options" />
                                <AIconButton :icon="IconFilter" label="Filters" />
                                <AIconButton
                                    :icon="IconStar"
                                    :pressed-icon="IconStarFilled"
                                    label="Favorites"
                                    :pressed="favorites"
                                    @click="favorites = !favorites"
                                />
                                <AIconButton :icon="IconSelectMode" label="Select" />
                            </template>
                            <template #meta>128 items</template>
                        </APageHeader>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="ANavItem, ANavLabel, ADivider" :min="280">
                    <KitDemo label="sidebar" stack>
                        <nav class="bg-sidebar flex flex-col gap-0.5 rounded-2xl p-3">
                            <ANavItem
                                to="/_kit"
                                :icon="IconHome"
                                :active-icon="IconHomeFilled"
                                label="Active (route)"
                            />
                            <ANavItem
                                to="/lists"
                                :icon="IconList"
                                :active-icon="IconListFilled"
                                label="Lists"
                                exact
                            />
                            <ANavLabel>Libraries</ANavLabel>
                            <ANavItem
                                :icon="IconBookshelf"
                                label="Button (click)"
                                @click="toast.show({ message: 'Clicked' })"
                            />
                            <ANavItem
                                :icon="IconBookshelf"
                                :active-icon="IconBookshelfFilled"
                                label="Active override"
                                active
                            />
                            <ANavItem :icon="IconDotsHorizontal" label="Disabled" disabled />
                            <ANavItem :icon="IconLink" label="Badge" :badge="3" @click="() => {}" />
                            <ADivider class="mx-3.5 my-2.5" />
                            <ANavItem to="/settings/interface" :icon="IconCog" label="Settings" />
                        </nav>
                    </KitDemo>
                    <KitDemo label="prefix, indent, wrap (table of contents)" stack>
                        <nav class="bg-sidebar flex flex-col gap-0.5 rounded-2xl p-3">
                            <ANavItem label="Part one" @click="() => {}">
                                <template #prefix>1</template>
                            </ANavItem>
                            <ANavItem
                                label="Chapter 1: A long chapter title that wraps onto a second line"
                                :indent="1"
                                wrap
                                active
                            />
                            <ANavItem label="Chapter 2" :indent="1" />
                            <ANavItem label="Section 2.1" :indent="2" />
                        </nav>
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Data -->
            <KitSection id="data" title="Data">
                <KitGroup title="ATable + ASortHeader" :min="500">
                    <KitDemo label="sortable, in a card" wide stack>
                        <ACard padding="none">
                            <ATable class="max-h-72">
                                <thead>
                                    <tr>
                                        <ASortHeader
                                            :sort="sortKey === 'name' ? sortDir : null"
                                            @toggle="toggleSort('name')"
                                            >Name</ASortHeader
                                        >
                                        <ASortHeader
                                            :sort="sortKey === 'status' ? sortDir : null"
                                            @toggle="toggleSort('status')"
                                            >Status</ASortHeader
                                        >
                                        <ASortHeader
                                            :sort="sortKey === 'created' ? sortDir : null"
                                            @toggle="toggleSort('created')"
                                            >Created</ASortHeader
                                        >
                                        <th><span class="sr-only">Actions</span></th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="row in sortedRows" :key="row.name">
                                        <td>{{ row.name }}</td>
                                        <td>
                                            <AChip size="sm" :tone="row.tone">{{
                                                row.status
                                            }}</AChip>
                                        </td>
                                        <td>{{ row.created }}</td>
                                        <td class="text-end">
                                            <AIconButton
                                                :icon="IconDelete"
                                                label="Delete"
                                                size="sm"
                                            />
                                        </td>
                                    </tr>
                                </tbody>
                            </ATable>
                        </ACard>
                    </KitDemo>
                    <KitDemo label="compact, empty" wide stack>
                        <ACard padding="none">
                            <ATable density="compact">
                                <thead>
                                    <tr>
                                        <th>Provider</th>
                                        <th>Account</th>
                                        <th>Linked</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr>
                                        <td colspan="3" class="text-fg-muted text-center">
                                            No linked accounts
                                        </td>
                                    </tr>
                                </tbody>
                            </ATable>
                        </ACard>
                    </KitDemo>
                </KitGroup>

                <KitGroup title="APagination" :min="360">
                    <KitDemo label="5 pages" stack>
                        <APagination v-model:page="pageA" :length="5" />
                    </KitDemo>
                    <KitDemo label="50 pages (compressed)" stack>
                        <APagination v-model:page="pageB" :length="50" />
                    </KitDemo>
                    <KitDemo :label="`clamps when length shrinks (length ${pageLength})`" stack>
                        <APagination v-model:page="pageC" :length="pageLength" />
                        <div class="flex gap-2">
                            <AButton
                                size="sm"
                                variant="tonal"
                                @click="pageLength = Math.max(1, pageLength - 3)"
                                >Shrink</AButton
                            >
                            <AButton size="sm" variant="tonal" @click="pageLength += 3"
                                >Grow</AButton
                            >
                        </div>
                    </KitDemo>
                </KitGroup>
            </KitSection>

            <!-- Media -->
            <KitSection id="media" title="Covers and rows">
                <KitGroup title="ACover" :min="150">
                    <KitDemo label="loaded, badges" stack>
                        <ACover
                            :src="coverSrc(25, 'One Piece')"
                            alt=""
                            :progress="0.6"
                            progress-text="60%"
                        >
                            <template #topLeft
                                ><ABadge :icon="IconBookOpenFilled" label="Reading"
                            /></template>
                            <template #topRight><ABadge label="12 unread">12</ABadge></template>
                        </ACover>
                    </KitDemo>
                    <KitDemo label="bottom-right chips" stack>
                        <ACover :src="coverSrc(61, 'Vinland Saga')" alt="">
                            <template #bottomRight>
                                <AChip size="sm" tone="success">+3</AChip>
                                <AChip size="sm" tone="info">~1</AChip>
                            </template>
                        </ACover>
                    </KitDemo>
                    <KitDemo label="loading (skeleton)" stack>
                        <div class="rounded-cover aspect-2/3 overflow-hidden">
                            <ASkeleton class="h-full" />
                        </div>
                    </KitDemo>
                    <KitDemo label="missing cover" stack>
                        <ACover :src="null" alt="Berserk" />
                    </KitDemo>
                    <KitDemo label="failed image" stack>
                        <ACover src="/_kit/missing.jpg" alt="Vagabond" />
                    </KitDemo>
                </KitGroup>

                <KitGroup
                    title="AScrollRow"
                    description="Cover cards as on the home page. The row scrolls with the keyboard when focused, and by dragging with a mouse."
                    :min="600"
                >
                    <KitDemo label="with cards" wide stack>
                        <AScrollRow title="Recently read">
                            <RouterLink
                                v-for="(title, i) in TITLES"
                                :key="title"
                                to="/_kit#media"
                                class="group flex flex-col gap-2.5"
                            >
                                <ACover
                                    :src="coverSrc(i * 37, title)"
                                    alt=""
                                    :progress="i % 3 === 0 ? 0.3 + i / 20 : undefined"
                                >
                                    <template v-if="i % 2 === 0" #topLeft
                                        ><ABadge :icon="IconBookOpenFilled" label="Reading"
                                    /></template>
                                    <template v-if="i % 4 === 1" #topRight
                                        ><ABadge>{{ i * 3 }}</ABadge></template
                                    >
                                </ACover>
                                <span
                                    class="line-clamp-2 px-0.5 text-sm leading-[1.35] font-medium group-hover:underline"
                                    >{{ title }}</span
                                >
                            </RouterLink>
                        </AScrollRow>
                    </KitDemo>
                    <KitDemo label="few items (buttons disabled)" wide stack>
                        <AScrollRow title="Newly added">
                            <div
                                v-for="(title, i) in TITLES.slice(0, 2)"
                                :key="title"
                                class="flex flex-col gap-2.5"
                            >
                                <ACover :src="coverSrc(i * 90 + 120, title)" alt="" />
                                <span class="line-clamp-2 text-sm font-medium">{{ title }}</span>
                            </div>
                        </AScrollRow>
                    </KitDemo>
                </KitGroup>
            </KitSection>
        </main>

        <ADialog
            v-model:open="dialogOpen"
            title="Rename list"
            description="Lists are visible to you only unless shared."
        >
            <div class="flex flex-col gap-4">
                <ATextField v-model="text" label="Name" autofocus />
                <ASelect v-model="status" :options="STATUS" label="Default status" />
            </div>
            <template #actions>
                <AButton variant="text" @click="dialogOpen = false">Cancel</AButton>
                <AButton @click="dialogOpen = false">Save</AButton>
            </template>
        </ADialog>
        <ADialog v-model:open="strictOpen" title="Scanning…" size="sm" :dismissible="false">
            Esc and outside clicks don't close this one.
            <template #actions>
                <AButton variant="text" tone="danger" @click="strictOpen = false">Stop</AButton>
            </template>
        </ADialog>
        <ADialog v-model:open="bareOpen" title="Cover" variant="bare">
            <img
                :src="coverSrc(25, 'One Piece')"
                alt="One Piece cover"
                class="rounded-cover max-h-[80dvh]"
            />
        </ADialog>
        <ADrawer v-model:open="drawerLeft" title="Navigation">
            <nav class="flex flex-col gap-0.5 p-3">
                <ANavItem to="/_kit" :icon="IconHome" :active-icon="IconHomeFilled" label="Kit" />
                <ANavItem to="/lists" :icon="IconList" label="Lists" />
            </nav>
        </ADrawer>
        <ADrawer v-slot="{ titleId }" v-model:open="drawerRight" side="right" :width="350">
            <div class="flex flex-col gap-4 p-4 pt-0">
                <div class="flex h-16 items-center justify-between">
                    <h2 :id="titleId" class="font-display text-xl">Reader</h2>
                    <AIconButton :icon="IconClose" label="Close" @click="drawerRight = false" />
                </div>
                <ASelect v-model="chapter" :options="CHAPTERS" label="Chapter" size="sm" />
                <ASlider
                    v-model="page"
                    label="Page"
                    show-value
                    :min="0"
                    :max="41"
                    :format-value="p => `${p + 1} of 42`"
                />
                <ASegmented v-model="readerMode" :options="READER_MODES" label="Mode" block />
            </div>
        </ADrawer>
        <AToastRegion />
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import AAlert from '@/ui/AAlert.vue'
import AAvatar from '@/ui/AAvatar.vue'
import ABadge from '@/ui/ABadge.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import AChip from '@/ui/AChip.vue'
import ACombobox from '@/ui/ACombobox.vue'
import ACover from '@/ui/ACover.vue'
import ADialog from '@/ui/ADialog.vue'
import ADivider from '@/ui/ADivider.vue'
import ADrawer from '@/ui/ADrawer.vue'
import AIcon from '@/ui/AIcon.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import AMenuSeparator from '@/ui/AMenuSeparator.vue'
import ANavItem from '@/ui/ANavItem.vue'
import ANavLabel from '@/ui/ANavLabel.vue'
import APageHeader from '@/ui/APageHeader.vue'
import APagination from '@/ui/APagination.vue'
import APopover from '@/ui/APopover.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import ARadioGroup from '@/ui/ARadioGroup.vue'
import AScrollRow from '@/ui/AScrollRow.vue'
import ASegmented from '@/ui/ASegmented.vue'
import ASelect from '@/ui/ASelect.vue'
import ASkeleton from '@/ui/ASkeleton.vue'
import ASlider from '@/ui/ASlider.vue'
import ASortHeader from '@/ui/ASortHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ASwitch from '@/ui/ASwitch.vue'
import ATable from '@/ui/ATable.vue'
import ATabs from '@/ui/ATabs.vue'
import ATextField from '@/ui/ATextField.vue'
import AToastRegion from '@/ui/AToastRegion.vue'
import ATooltip from '@/ui/ATooltip.vue'
import * as icons from '@/ui/icons'
import {
    IconAlert,
    IconBookOpen,
    IconBookOpenFilled,
    IconBookshelf,
    IconBookshelfFilled,
    IconChevronDown,
    IconClose,
    IconCog,
    IconContentCopy,
    IconDelete,
    IconDotsHorizontal,
    IconDotsVertical,
    IconDownload,
    IconFilter,
    IconHome,
    IconHomeFilled,
    IconLink,
    IconList,
    IconListFilled,
    IconMagnify,
    IconMinus,
    IconPencil,
    IconPlus,
    IconSelectMode,
    IconSelectModeFilled,
    IconStar,
    IconStarFilled,
    IconSync,
    IconTune,
    IconWeatherNight,
    IconWeatherSunny,
} from '@/ui/icons'
import { usePressAndHold } from '@/ui/usePressAndHold'
import { useToast } from '@/ui/useToast'
import KitDemo from './KitDemo.vue'
import KitGroup from './KitGroup.vue'
import KitSection from './KitSection.vue'

/** The living spec of the kit (dev only): every component in every state. */
useHead({ title: 'Kit' })

const layout = useLayoutStore()
const toast = useToast()

const SECTIONS = [
    { id: 'foundations', title: 'Foundations' },
    { id: 'actions', title: 'Actions' },
    { id: 'inputs', title: 'Text inputs' },
    { id: 'selection', title: 'Selection' },
    { id: 'feedback', title: 'Feedback' },
    { id: 'overlays', title: 'Overlays' },
    { id: 'structure', title: 'Structure' },
    { id: 'data', title: 'Data' },
    { id: 'media', title: 'Covers' },
]
const THEMES = [
    { value: 'light', label: 'Light', icon: IconWeatherSunny },
    { value: 'dark', label: 'Dark', icon: IconWeatherNight },
] as const

const COLOR_TOKENS = [
    'primary',
    'on-primary',
    'primary-container',
    'on-primary-container',
    'secondary-container',
    'on-secondary-container',
    'bg',
    'surface-1',
    'surface-2',
    'surface-3',
    'surface-4',
    'raised',
    'sidebar',
    'search',
    'field',
    'card-line',
    'fg',
    'fg-muted',
    'outline',
    'outline-variant',
    'inverse',
    'on-inverse',
    'inverse-primary',
    'error',
    'error-container',
    'success',
    'success-container',
    'warning',
    'warning-container',
    'info',
    'info-container',
    'star',
]
const ICONS = Object.entries(icons)

const BUTTON_VARIANTS = ['filled', 'tonal', 'outlined', 'text'] as const
const ICON_BUTTON_VARIANTS = ['standard', 'tonal', 'filled', 'outlined'] as const
const ALERT_TONES = ['info', 'success', 'warning', 'danger'] as const
const CHIP_TONES = ['neutral', 'primary', 'success', 'warning', 'danger', 'info'] as const

const VISIBILITY = [
    { value: 'show', label: 'Show' },
    { value: 'overflow', label: 'Overflow' },
    { value: 'hide', label: 'Hide' },
] as const
const COUNT_MODES = [
    { value: 'unread', label: 'Unread' },
    { value: 'total', label: 'Total' },
] as const
const READER_MODES = [
    { value: 'paged', label: 'Paged' },
    { value: 'longstrip', label: 'Longstrip' },
    { value: 'auto', label: 'Auto' },
] as const
const KIT_TABS = [
    { value: 'contents', label: 'Contents' },
    { value: 'settings', label: 'Settings' },
    { value: 'about', label: 'About' },
]
const STATUS = [
    { value: 'reading', label: 'Reading', icon: IconBookOpen },
    { value: 'completed', label: 'Completed' },
    { value: 'on_hold', label: 'On hold' },
    { value: 'dropped', label: 'Dropped', disabled: true },
    { value: 'plan_to_read', label: 'Plan to read' },
]
const CHAPTERS = Array.from({ length: 30 }, (_, i) => ({
    value: `c${i + 1}`,
    label: `Chapter ${i + 1}${i % 7 === 3 ? ': The one with a much longer title' : ''}`,
}))
const MANY_CHAPTERS = Array.from({ length: 500 }, (_, i) => ({
    value: `c${i + 1}`,
    label: `Chapter ${i + 1}${i % 7 === 3 ? ': The one with a much longer title that has to be cut off' : ''}`,
}))
const URIS = [
    'comic/one-piece/v01.cbz',
    'comic/one-piece/v02.cbz',
    'comic/berserk/v01.cbz',
    'book/dune.epub',
    'book/the-hobbit.epub',
]
const ACTIONS = [
    { value: 'reset', label: 'Reset progress' },
    { value: 'mark_all', label: 'Mark all as read' },
    { value: 'mark_until', label: 'Mark read until…' },
]
const ACTIONS_DESCRIBED = [
    { value: 'reset', label: 'Reset progress', description: 'Every chapter becomes unread.' },
    { value: 'mark_all', label: 'Mark all as read', description: 'Every chapter becomes read.' },
    {
        value: 'mark_until',
        label: 'Mark read until…',
        disabled: true,
        description: 'Needs chapters.',
    },
]
const TITLES = [
    'One Piece',
    'Berserk',
    'Vagabond',
    'Dune',
    'The Hobbit',
    'Frieren: Beyond Journey’s End',
    'Monster',
    'Pluto',
    'Blame!',
    'Dorohedoro',
]

const favorites = ref(false)
const noValue = ref<string | null>(null)
const visibility = ref<string>('show')
const countMode = ref<string>('unread')
const readerMode = ref<string>('auto')
const kitTab = ref('contents')
const columns = ref(6)
const minusHold = usePressAndHold(() => columns.value > 1 && columns.value--)
const plusHold = usePressAndHold(() => columns.value < 20 && columns.value++)

const text = ref('tijl')
const empty = ref('')
const search = ref('berserk')
const days = ref<number | null>(30)
const notes = ref('Line one\nLine two')

const status = ref<string | null>('reading')
const statusEmpty = ref<string | null>(null)
const chapter = ref<string | null>('c4')
const manyChapter = ref<string | null>('c250')
const provider = ref<'oidc' | 'proxy' | null>('oidc')
const uri = ref<string | null>(null)
const uriSet = ref<string | null>('book/dune.epub')

const check1 = ref(false)
const check2 = ref(true)
const check3 = ref(false)
const lastShift = ref(false)
const switch1 = ref(false)
const switch2 = ref(true)
const action = ref<string | null>('reset')
const page = ref(12)
const width = ref(60)
const committed = ref<number | null>(null)

const alertDismissed = ref(false)
const chipOn = ref(false)

const popoverOpen = ref(false)
const dialogOpen = ref(false)
const strictOpen = ref(false)
const bareOpen = ref(false)
const drawerLeft = ref(false)
const drawerRight = ref(false)

type Row = { name: string; status: string; tone: 'success' | 'danger' | 'info'; created: string }
const ROWS: Row[] = [
    { name: 'Scan library', status: 'Completed', tone: 'success', created: '2026-09-20' },
    { name: 'Fetch metadata', status: 'Failed', tone: 'danger', created: '2026-09-22' },
    { name: 'Generate covers', status: 'Running', tone: 'info', created: '2026-09-24' },
    { name: 'Clean up', status: 'Completed', tone: 'success', created: '2026-09-18' },
]
const sortKey = ref<'name' | 'status' | 'created'>('created')
const sortDir = ref<'asc' | 'desc'>('desc')
function toggleSort(key: typeof sortKey.value) {
    if (sortKey.value === key) sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
    else [sortKey.value, sortDir.value] = [key, 'asc']
}
const sortedRows = computed(() => {
    const sorted = ROWS.toSorted((a, b) => a[sortKey.value].localeCompare(b[sortKey.value]))
    return sortDir.value === 'asc' ? sorted : sorted.reverse()
})

const pageA = ref(2)
const pageB = ref(24)
const pageC = ref(9)
const pageLength = ref(10)

function coverGradient(hue: number) {
    return `linear-gradient(160deg, oklch(0.7 0.12 ${hue}), oklch(0.35 0.1 ${hue + 40}))`
}

function coverSrc(hue: number, title: string) {
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 300">
        <defs><linearGradient id="g" x1="0" y1="0" x2="0.4" y2="1">
            <stop offset="0" stop-color="oklch(0.72 0.12 ${hue})"/>
            <stop offset="1" stop-color="oklch(0.32 0.09 ${hue + 40})"/>
        </linearGradient></defs>
        <rect width="200" height="300" fill="url(#g)"/>
        <text x="16" y="260" fill="white" font-family="Georgia, serif" font-size="22" font-weight="600">${title.replace(/[<&]/g, '')}</text>
    </svg>`
    return `data:image/svg+xml,${encodeURIComponent(svg)}`
}
</script>

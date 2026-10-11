package me.tijlvdb.voltis.ui.reader

import androidx.activity.compose.LocalActivity
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.EnterExitState
import androidx.compose.foundation.layout.Arrangement
import androidx.navigation3.ui.LocalNavAnimatedContentScope
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.traversalIndex
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import kotlin.math.roundToInt
import kotlinx.coroutines.delay
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.domain.comic.Siblings
import me.tijlvdb.voltis.ui.LocalSnackbarLift
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIconButtonStyle
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VOverflowMenu
import me.tijlvdb.voltis.ui.kit.VProgressBar
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.kit.popoverAnchor
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester

/**
 * The bars over the page, hidden until a centre tap, and the edge progress bar, which stays.
 * The system bars show only with them: see [ReaderBars].
 */
@Composable
fun ReaderChrome(
    visible: Boolean,
    title: String,
    series: Content?,
    page: Int,
    pageCount: Int,
    /** Paged right-to-left: the slider and the progress bar are mirrored. */
    rtl: Boolean,
    siblings: Siblings,
    /** The previous and next volume when they can be opened now. */
    prev: Content?,
    next: Content?,
    /** TalkBack can't find the tap zones by touch: the bottom bar gets buttons for them. */
    pageButtons: Boolean,
    onBack: () -> Unit,
    onSettings: () -> Unit,
    /** Marks the settings button, which TalkBack returns to when the settings close. */
    settingsAnchor: PopoverAnchor? = null,
    settingsFocus: FocusRequester = remember { FocusRequester() },
    onMove: (forward: Boolean) -> Unit,
    onPlace: (Int) -> Unit,
    onVolume: (String) -> Unit,
    /** The previous or next volume, asked once more whether it can be opened. */
    onAdjacent: (String) -> Unit,
    onDetails: () -> Unit,
    onRetrySiblings: () -> Unit,
) {
    var picking by rememberSaveable { mutableStateOf(false) }
    val hasVolumes = siblings.status == Siblings.Status.Ready && siblings.items.isNotEmpty()
    val colors = MaterialTheme.colorScheme
    // The bottom bar's height with the system inset, for what floats above it.
    var barHeight by remember { mutableIntStateOf(0) }

    Box(Modifier.fillMaxSize()) {
        AnimatedVisibility(visible, Modifier.align(Alignment.TopCenter).semantics { isTraversalGroup = true; traversalIndex = -1f }, fadeIn(), fadeOut()) {
            VTopBar(
                title,
                onBack,
                overlay = true,
                onTitleClick = if (hasVolumes) ({ picking = true }) else null,
                titleClickLabel = stringResource(R.string.reader_volumes),
                actions = {
                    if (hasVolumes && siblings.index >= 0) {
                        Text(
                            stringResource(R.string.reader_volume_of, siblings.index + 1, siblings.items.size, siblings.items.size - siblings.index - 1),
                            Modifier.padding(end = 16.dp),
                            color = colors.onSurfaceVariant,
                            style = MaterialTheme.typography.labelMedium,
                        )
                    }
                },
            )
        }

        // Over the page, above the bottom bar. After the page in TalkBack's order, before the bar.
        AnimatedVisibility(
            visible,
            Modifier.align(Alignment.BottomEnd).semantics { isTraversalGroup = true; traversalIndex = 0.5f },
            fadeIn(),
            fadeOut(),
        ) {
            Row(
                Modifier
                    .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.End))
                    .padding(end = 16.dp, bottom = with(LocalDensity.current) { barHeight.toDp() } + 16.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                VIconButton(
                    VIcons.Cog,
                    stringResource(R.string.reader_settings),
                    onSettings,
                    Modifier.focusRequester(settingsFocus).then(if (settingsAnchor != null) Modifier.popoverAnchor(settingsAnchor) else Modifier),
                    style = VIconButtonStyle.Floating,
                )
                VOverflowMenu(
                    listOfNotNull(
                        (stringResource(R.string.reader_volumes) to { picking = true }).takeIf { hasVolumes },
                        stringResource(R.string.reader_details) to onDetails,
                    ),
                    style = VIconButtonStyle.Floating,
                )
            }
        }

        AnimatedVisibility(visible, Modifier.align(Alignment.BottomCenter).semantics { isTraversalGroup = true; traversalIndex = 1f }, fadeIn(), fadeOut()) {
            // Snackbars rise above the bar while it shows.
            val lift = LocalSnackbarLift.current
            val density = LocalDensity.current
            val inset = WindowInsets.safeDrawing.getBottom(density)
            DisposableEffect(lift) { onDispose { lift.value = 0.dp } }
            Surface(
                Modifier.onSizeChanged { barHeight = it.height; lift.value = with(density) { (it.height - inset).coerceAtLeast(0).toDp() } },
                color = colors.surfaceContainerLow,
            ) {
                Column(
                    Modifier
                        .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
                        .padding(horizontal = 4.dp),
                ) {
                    if (siblings.status == Siblings.Status.Error) {
                        Row(Modifier.padding(start = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                stringResource(R.string.reader_siblings_error),
                                Modifier.weight(1f),
                                color = colors.onSurfaceVariant,
                                style = MaterialTheme.typography.bodyMedium,
                            )
                            VButton(stringResource(R.string.retry), onRetrySiblings, style = VButtonStyle.Text)
                        }
                    }
                    if (pageButtons) {
                        Row(Modifier.fillMaxWidth(), Arrangement.SpaceEvenly) {
                            VButton(stringResource(R.string.reader_prev_page), { onMove(false) }, style = VButtonStyle.Text)
                            VButton(stringResource(R.string.reader_next_page), { onMove(true) }, style = VButtonStyle.Text)
                        }
                    }
                    // Physical, like the chevrons; the slider alone follows the reading direction.
                    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            if (hasVolumes) {
                                VIconButton(
                                    VIcons.ChevronLeft,
                                    stringResource(R.string.reader_prev_volume),
                                    { prev?.let { onAdjacent(it.id) } },
                                    style = VIconButtonStyle.Subtle,
                                    enabled = prev != null,
                                )
                            }
                            PageSlider(page, pageCount, rtl, onPlace, Modifier.weight(1f).padding(horizontal = 12.dp))
                            if (hasVolumes) {
                                VIconButton(
                                    VIcons.ChevronRight,
                                    stringResource(R.string.reader_next_volume),
                                    { next?.let { onAdjacent(it.id) } },
                                    style = VIconButtonStyle.Subtle,
                                    enabled = next != null,
                                )
                            }
                        }
                    }
                }
            }
        }

        val fraction = if (pageCount > 0) page.toFloat() / pageCount else 0f
        val progress = stringResource(R.string.reader_progress)
        VProgressBar(
            fraction,
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(3.dp).semantics {
                contentDescription = progress
                progressBarRangeInfo = ProgressBarRangeInfo(fraction, 0f..1f)
            },
            reversed = rtl,
        )
    }

    if (picking && hasVolumes) {
        VolumeSheet(
            stringResource(R.string.reader_volumes),
            siblings.items,
            series,
            siblings.index,
            onPick = {
                picking = false
                onVolume(it)
            },
            onDismiss = { picking = false },
        )
    }
}

/** The label follows the thumb at once; the page is placed once it pauses, so it stays visible while scrubbing. */
@Composable
private fun PageSlider(page: Int, pageCount: Int, rtl: Boolean, onPlace: (Int) -> Unit, modifier: Modifier) {
    var scrub by remember { mutableStateOf<Int?>(null) }
    LaunchedEffect(scrub) {
        val to = scrub ?: return@LaunchedEffect
        delay(200)
        onPlace(to)
    }
    val shown = scrub ?: page
    val label = stringResource(R.string.reader_page_of, shown + 1, pageCount)
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        if (pageCount > 1) {
            val name = stringResource(R.string.reader_page_slider)
            CompositionLocalProvider(LocalLayoutDirection provides if (rtl) LayoutDirection.Rtl else LayoutDirection.Ltr) {
                Slider(
                    value = shown.toFloat(),
                    onValueChange = { scrub = it.roundToInt() },
                    modifier = Modifier.weight(1f).semantics {
                        contentDescription = name
                        stateDescription = label
                    },
                    valueRange = 0f..(pageCount - 1).toFloat(),
                    onValueChangeFinished = {
                        scrub?.let(onPlace)
                        scrub = null
                    },
                )
            }
        }
        Text(
            label,
            Modifier.padding(start = 12.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

/**
 * Held by the nav while a reader is the top screen, so a volume replacing a reader never lets the bars
 * through in between: the screen stays on and the bars are put back when the reader is left.
 */
@Composable
fun ReaderWindow(reading: Boolean) {
    val view = LocalView.current
    val window = LocalActivity.current?.window ?: return
    val controller = remember(window, view) { WindowCompat.getInsetsController(window, view) }
    DisposableEffect(controller, reading) {
        if (!reading) return@DisposableEffect onDispose {}
        view.keepScreenOn = true
        controller.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        onDispose {
            view.keepScreenOn = false
            controller.show(WindowInsetsCompat.Type.systemBars())
        }
    }
}

/**
 * The system bars follow the menu: shown with it, hidden without. Composed for the reader's whole screen, loading
 * included; a screen on its way out (closed, or replaced by another volume) leaves them to the one that stays.
 */
@Composable
fun ReaderBars(visible: Boolean) {
    if (LocalNavAnimatedContentScope.current.transition.targetState != EnterExitState.Visible) return
    val view = LocalView.current
    val window = LocalActivity.current?.window ?: return
    val controller = remember(window, view) { WindowCompat.getInsetsController(window, view) }
    LaunchedEffect(controller, visible) {
        val bars = WindowInsetsCompat.Type.systemBars()
        if (visible) controller.show(bars) else controller.hide(bars)
    }
}

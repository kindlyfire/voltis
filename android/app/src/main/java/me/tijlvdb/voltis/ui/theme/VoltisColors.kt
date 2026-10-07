package me.tijlvdb.voltis.ui.theme

import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** The tokens Material's ColorScheme has no role for. Read as `VoltisTheme.colors`. */
@Immutable
data class VoltisColors(
    val star: Color,
    val raised: Color,
    val search: Color,
    val field: Color,
    val cardLine: Color,
    val infoContainer: Color,
    val onInfoContainer: Color,
    val warningContainer: Color,
    val onWarningContainer: Color,
    val successContainer: Color,
    val onSuccessContainer: Color,
    /** The two layers of `--shadow-reading`, inner then outer. */
    val readingGlow: Pair<Glow, Glow>,
)

/** One layer of a glow: a CSS box shadow without an offset. */
@Immutable
data class Glow(val color: Color, val blur: Dp, val spread: Dp)

val LightVoltisColors = VoltisColors(
    star = LightStar,
    raised = LightRaised,
    search = LightSearch,
    field = LightField,
    cardLine = LightCardLine,
    infoContainer = LightInfoContainer,
    onInfoContainer = LightOnInfoContainer,
    warningContainer = LightWarningContainer,
    onWarningContainer = LightOnWarningContainer,
    successContainer = LightSuccessContainer,
    onSuccessContainer = LightOnSuccessContainer,
    readingGlow = Glow(LightPrimary.copy(alpha = 0.30f), 3.dp, 1.dp) to Glow(LightPrimary.copy(alpha = 0.35f), 12.dp, 3.dp),
)

val DarkVoltisColors = VoltisColors(
    star = DarkStar,
    raised = DarkRaised,
    search = DarkSearch,
    field = DarkField,
    cardLine = DarkCardLine,
    infoContainer = DarkInfoContainer,
    onInfoContainer = DarkOnInfoContainer,
    warningContainer = DarkWarningContainer,
    onWarningContainer = DarkOnWarningContainer,
    successContainer = DarkSuccessContainer,
    onSuccessContainer = DarkOnSuccessContainer,
    readingGlow = Glow(DarkPrimary.copy(alpha = 0.55f), 4.dp, 1.dp) to Glow(DarkPrimary.copy(alpha = 0.60f), 15.dp, 3.dp),
)

val LocalVoltisColors = staticCompositionLocalOf { LightVoltisColors }

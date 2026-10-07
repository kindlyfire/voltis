package me.tijlvdb.voltis.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color

// The web has no secondary or tertiary: both take the primary.
private val LightScheme = lightColorScheme(
    primary = LightPrimary,
    onPrimary = LightOnPrimary,
    primaryContainer = LightPrimaryContainer,
    onPrimaryContainer = LightOnPrimaryContainer,
    inversePrimary = LightInversePrimary,
    secondary = LightPrimary,
    onSecondary = LightOnPrimary,
    secondaryContainer = LightSecondaryContainer,
    onSecondaryContainer = LightOnSecondaryContainer,
    tertiary = LightPrimary,
    onTertiary = LightOnPrimary,
    tertiaryContainer = LightPrimaryContainer,
    onTertiaryContainer = LightOnPrimaryContainer,
    background = LightBg,
    onBackground = LightFg,
    surface = LightBg,
    onSurface = LightFg,
    surfaceVariant = LightSurface3,
    onSurfaceVariant = LightFgMuted,
    surfaceTint = LightPrimary,
    inverseSurface = LightInverse,
    inverseOnSurface = LightOnInverse,
    error = LightError,
    onError = LightOnError,
    errorContainer = LightErrorContainer,
    onErrorContainer = LightOnErrorContainer,
    outline = LightOutline,
    outlineVariant = LightOutlineVariant,
    scrim = Color.Black,
    surfaceBright = LightRaised,
    surfaceDim = LightBg,
    surfaceContainerLowest = LightRaised,
    surfaceContainerLow = LightSurface1,
    surfaceContainer = LightSurface2,
    surfaceContainerHigh = LightSurface3,
    surfaceContainerHighest = LightSurface4,
)

private val DarkScheme = darkColorScheme(
    primary = DarkPrimary,
    onPrimary = DarkOnPrimary,
    primaryContainer = DarkPrimaryContainer,
    onPrimaryContainer = DarkOnPrimaryContainer,
    inversePrimary = DarkInversePrimary,
    secondary = DarkPrimary,
    onSecondary = DarkOnPrimary,
    secondaryContainer = DarkSecondaryContainer,
    onSecondaryContainer = DarkOnSecondaryContainer,
    tertiary = DarkPrimary,
    onTertiary = DarkOnPrimary,
    tertiaryContainer = DarkPrimaryContainer,
    onTertiaryContainer = DarkOnPrimaryContainer,
    background = DarkBg,
    onBackground = DarkFg,
    surface = DarkBg,
    onSurface = DarkFg,
    surfaceVariant = DarkSurface3,
    onSurfaceVariant = DarkFgMuted,
    surfaceTint = DarkPrimary,
    inverseSurface = DarkInverse,
    inverseOnSurface = DarkOnInverse,
    error = DarkError,
    onError = DarkOnError,
    errorContainer = DarkErrorContainer,
    onErrorContainer = DarkOnErrorContainer,
    outline = DarkOutline,
    outlineVariant = DarkOutlineVariant,
    scrim = Color.Black,
    surfaceBright = DarkRaised,
    surfaceDim = DarkBg,
    surfaceContainerLowest = DarkRaised,
    surfaceContainerLow = DarkSurface1,
    surfaceContainer = DarkSurface2,
    surfaceContainerHigh = DarkSurface3,
    surfaceContainerHighest = DarkSurface4,
)

@Composable
fun VoltisTheme(dark: Boolean, content: @Composable () -> Unit) {
    CompositionLocalProvider(LocalVoltisColors provides if (dark) DarkVoltisColors else LightVoltisColors) {
        MaterialTheme(
            colorScheme = if (dark) DarkScheme else LightScheme,
            shapes = MaterialShapes,
            typography = VoltisTypography,
            content = content,
        )
    }
}

object VoltisTheme {
    val colors: VoltisColors
        @Composable @ReadOnlyComposable get() = LocalVoltisColors.current
}

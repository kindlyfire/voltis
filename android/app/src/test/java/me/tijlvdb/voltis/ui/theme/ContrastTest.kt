package me.tijlvdb.voltis.ui.theme

import androidx.compose.ui.graphics.Color
import kotlin.math.pow
import org.junit.Assert.assertEquals
import org.junit.Test

/** WCAG 2 relative luminance of an sRGB color. */
private fun luminance(color: Color): Double {
    fun linear(c: Float) = if (c <= 0.04045f) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)
    return 0.2126 * linear(color.red) + 0.7152 * linear(color.green) + 0.0722 * linear(color.blue)
}

private fun contrast(a: Color, b: Color): Double {
    val (hi, lo) = listOf(luminance(a), luminance(b)).sortedDescending()
    return (hi + 0.05) / (lo + 0.05)
}

/**
 * Pins the contrast of the pairs the app draws, in both palettes. The tokens are the web's: a pair that
 * fails goes into [knownFailures] with its issue, and no token is changed to make this pass.
 */
class ContrastTest {
    private class Check(val name: String, val fg: Color, val bg: Color, val min: Double)

    private fun pairs(theme: String, p: Map<String, Color>): List<Check> {
        val out = mutableListOf<Check>()
        fun add(fg: String, bg: String, min: Double = 4.5) = out.add(Check("$theme $fg on $bg", p.getValue(fg), p.getValue(bg), min))
        for (fg in listOf("fg", "fg-muted")) {
            for (bg in listOf("bg", "surface-1", "surface-2", "surface-3", "surface-4", "raised")) add(fg, bg)
        }
        for (role in listOf("primary", "error", "success", "warning", "info")) add("on-$role", role)
        for (role in listOf("primary", "secondary", "error", "success", "warning", "info")) add("on-$role-container", "$role-container")
        add("on-inverse", "inverse")
        // A tag on a card.
        add("fg", "field")
        for (fg in listOf("primary", "error")) {
            for (bg in listOf("bg", "surface-1")) add(fg, bg)
        }
        for (bg in listOf("bg", "surface-1")) add("star", bg, 3.0)
        add("outline", "bg", 3.0)
        return out
    }

    private val light = mapOf(
        "fg" to LightFg, "fg-muted" to LightFgMuted, "bg" to LightBg, "raised" to LightRaised, "field" to LightField,
        "surface-1" to LightSurface1, "surface-2" to LightSurface2, "surface-3" to LightSurface3, "surface-4" to LightSurface4,
        "primary" to LightPrimary, "on-primary" to LightOnPrimary,
        "primary-container" to LightPrimaryContainer, "on-primary-container" to LightOnPrimaryContainer,
        "secondary-container" to LightSecondaryContainer, "on-secondary-container" to LightOnSecondaryContainer,
        "error" to LightError, "on-error" to LightOnError,
        "error-container" to LightErrorContainer, "on-error-container" to LightOnErrorContainer,
        "success" to LightSuccess, "on-success" to LightOnSuccess,
        "success-container" to LightSuccessContainer, "on-success-container" to LightOnSuccessContainer,
        "warning" to LightWarning, "on-warning" to LightOnWarning,
        "warning-container" to LightWarningContainer, "on-warning-container" to LightOnWarningContainer,
        "info" to LightInfo, "on-info" to LightOnInfo,
        "info-container" to LightInfoContainer, "on-info-container" to LightOnInfoContainer,
        "inverse" to LightInverse, "on-inverse" to LightOnInverse,
        "star" to LightStar, "outline" to LightOutline,
    )

    private val dark = mapOf(
        "fg" to DarkFg, "fg-muted" to DarkFgMuted, "bg" to DarkBg, "raised" to DarkRaised, "field" to DarkField,
        "surface-1" to DarkSurface1, "surface-2" to DarkSurface2, "surface-3" to DarkSurface3, "surface-4" to DarkSurface4,
        "primary" to DarkPrimary, "on-primary" to DarkOnPrimary,
        "primary-container" to DarkPrimaryContainer, "on-primary-container" to DarkOnPrimaryContainer,
        "secondary-container" to DarkSecondaryContainer, "on-secondary-container" to DarkOnSecondaryContainer,
        "error" to DarkError, "on-error" to DarkOnError,
        "error-container" to DarkErrorContainer, "on-error-container" to DarkOnErrorContainer,
        "success" to DarkSuccess, "on-success" to DarkOnSuccess,
        "success-container" to DarkSuccessContainer, "on-success-container" to DarkOnSuccessContainer,
        "warning" to DarkWarning, "on-warning" to DarkOnWarning,
        "warning-container" to DarkWarningContainer, "on-warning-container" to DarkOnWarningContainer,
        "info" to DarkInfo, "on-info" to DarkOnInfo,
        "info-container" to DarkInfoContainer, "on-info-container" to DarkOnInfoContainer,
        "inverse" to DarkInverse, "on-inverse" to DarkOnInverse,
        "star" to DarkStar, "outline" to DarkOutline,
    )

    /** Pairs below their minimum today, each with its issue, e.g. "Light star on bg" to "voltis-xyz". */
    private val knownFailures = emptyMap<String, String>()

    @Test
    fun pairsMeetTheirMinimum() {
        val all = pairs("Light", light) + pairs("Dark", dark)
        val failing = all.filter { contrast(it.fg, it.bg) < it.min }
            .associate { it.name to "%.2f < %.1f".format(contrast(it.fg, it.bg), it.min) }
        assertEquals("Failing: $failing", knownFailures.keys, failing.keys)
        // A sanity anchor for the formula: the light text on its background.
        assertEquals(16.12, contrast(LightFg, LightBg), 0.01)
    }
}

package me.tijlvdb.voltis.ui.theme

import java.io.File
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.pow
import kotlin.math.roundToInt
import kotlin.math.sin
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * CSS Color 4: OKLCH to OKLab to linear sRGB, each channel clipped to 0..1 (what a browser shows
 * for the tokens slightly outside sRGB), then gamma-encoded.
 */
fun oklchToSrgb(l: Double, c: Double, h: Double): List<Int> {
    val a = c * cos(Math.toRadians(h))
    val b = c * sin(Math.toRadians(h))
    val l3 = (l + 0.3963377774 * a + 0.2158037573 * b).pow(3)
    val m3 = (l - 0.1055613458 * a - 0.0638541728 * b).pow(3)
    val s3 = (l - 0.0894841775 * a - 1.2914855480 * b).pow(3)
    return listOf(
        4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3,
        -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3,
        -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3,
    ).map { linear ->
        val v = linear.coerceIn(0.0, 1.0)
        ((if (v <= 0.0031308) 12.92 * v else 1.055 * v.pow(1 / 2.4) - 0.055) * 255).roundToInt()
    }
}

class ThemeTokensTest {
    private val cssToken = Regex("""--color-([a-z0-9-]+):\s*oklch\(([\d.]+) ([\d.]+) ([\d.]+)\);""")
    private val kotlinToken = Regex("""val (\w+) = Color\(0xFF([0-9A-F]{6})\)""")

    /** Kotlin name to sRGB, for the tokens of one block of tokens.css. */
    private fun cssTokens(block: String, prefix: String): Map<String, List<Int>> {
        val tokens = cssToken.findAll(block).associate { m ->
            val (name, l, c, h) = m.destructured
            val kotlin = prefix + name.split('-').joinToString("") { it.replaceFirstChar(Char::uppercaseChar) }
            kotlin to oklchToSrgb(l.toDouble(), c.toDouble(), h.toDouble())
        }
        // A token in another notation (alpha, var(), hex) must fail here, not be skipped.
        assertEquals("$prefix tokens that aren't oklch(L C H)", Regex("""--color-[a-z0-9-]+:""").findAll(block).count(), tokens.size)
        return tokens
    }

    @Test
    fun colorsMatchTokensCss() {
        // Gradle runs unit tests in android/app.
        val css = File("../../frontend/src/ui/tokens.css").readText()
        val want = cssTokens(css.substringAfter("@theme static").substringBefore("@layer base"), "Light") +
            cssTokens(css.substringAfter(".dark {"), "Dark")
        val have = kotlinToken.findAll(File("src/main/java/me/tijlvdb/voltis/ui/theme/Color.kt").readText()).associate { m ->
            m.groupValues[1] to m.groupValues[2].chunked(2).map { it.toInt(16) }
        }

        assertEquals(want.keys, have.keys)
        for ((name, rgb) in want) {
            val close = rgb.zip(have.getValue(name)).all { (a, b) -> abs(a - b) <= 1 }
            assertTrue("$name: tokens.css gives $rgb, Color.kt has ${have[name]}", close)
        }
    }
}

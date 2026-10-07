package me.tijlvdb.voltis.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import me.tijlvdb.voltis.R

// Variable fonts: each weight in use needs its own Font entry with the axis set.
private fun family(res: Int, vararg weights: Int) = FontFamily(
    weights.map { Font(res, FontWeight(it), variationSettings = FontVariation.Settings(FontVariation.weight(it))) },
)

private val Figtree = family(R.font.figtree, 400, 500, 600)
private val SourceSerif = family(R.font.source_serif_4, 600)

private fun serif(size: Int, line: Int) =
    TextStyle(fontFamily = SourceSerif, fontWeight = FontWeight.SemiBold, fontSize = size.sp, lineHeight = line.sp)

private fun sans(size: Int, line: Int, weight: FontWeight = FontWeight.Normal) =
    TextStyle(fontFamily = Figtree, fontWeight = weight, fontSize = size.sp, lineHeight = line.sp)

private val base = Typography()

val VoltisTypography = Typography(
    displayLarge = base.displayLarge.copy(fontFamily = SourceSerif),
    displayMedium = serif(40, 46),
    displaySmall = serif(32, 38),
    headlineLarge = base.headlineLarge.copy(fontFamily = SourceSerif),
    headlineMedium = serif(30, 36),
    headlineSmall = serif(24, 29),
    titleLarge = serif(22, 28),
    titleMedium = serif(20, 26),
    titleSmall = sans(14, 20, FontWeight.SemiBold),
    bodyLarge = sans(15, 22),
    bodyMedium = sans(14, 20),
    bodySmall = sans(13, 18),
    labelLarge = sans(14, 20, FontWeight.SemiBold),
    labelMedium = sans(12, 16, FontWeight.SemiBold),
    labelSmall = base.labelSmall.copy(fontFamily = Figtree),
)

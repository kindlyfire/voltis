package me.tijlvdb.voltis.domain.comic

import me.tijlvdb.voltis.data.api.DisplayMetadata

// A port of `pages/read/ComicDisplay/direction.ts`, pinned by WebSourcePinTest.

enum class ReadingDirection { Ltr, Rtl }

// `Yes` and `Unknown` say nothing about page order: taggers write `Yes` for any manga.
private fun fromManga(manga: String?): ReadingDirection? = when (manga?.lowercase()) {
    "yesandrighttoleft" -> ReadingDirection.Rtl
    "no" -> ReadingDirection.Ltr
    else -> null
}

/**
 * The file's own tag beats the series' (a series can mix flipped and unflipped volumes); the
 * series kind is the weakest signal.
 */
fun detectDirection(comic: DisplayMetadata?, series: DisplayMetadata?): ReadingDirection =
    fromManga(comic?.manga)
        ?: fromManga(series?.manga)
        ?: if (series?.kind == "manga") ReadingDirection.Rtl else ReadingDirection.Ltr

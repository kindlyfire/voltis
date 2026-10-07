package me.tijlvdb.voltis.domain.comic

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject
import me.tijlvdb.voltis.domain.numberOrNull

enum class Fit { Screen, Width, Height }

enum class SpreadSetting { Single, Double, Auto }

/** What a series (or a parentless comic) overrides; null is Auto. */
data class SeriesSettings(val mode: ReaderMode? = null, val direction: ReadingDirection? = null)

/** The comic reader's settings of this device, in the shape the web keeps under `reader:comics`. */
data class ReaderSettings(
    /** Percent of the viewport, 10 to 100. */
    val longstripWidth: Int = 100,
    val fit: Fit = Fit.Screen,
    val spread: SpreadSetting = SpreadSetting.Auto,
    val zoomWide: Boolean = true,
    /** RTL also mirrors tap zones and swipes. */
    val invertRtlControls: Boolean = true,
    /** Mode is keyed by `parentId ?: ""`, direction by `parentId ?: id`. */
    val seriesSettings: Map<String, SeriesSettings> = emptyMap(),
    /** Comics whose spreads are shifted by one page. */
    val shiftedBooks: Set<String> = emptySet(),
) {
    /** Changes the entry of [key], dropping it once every field is back on Auto. */
    fun updateSeriesSettings(key: String, change: (SeriesSettings) -> SeriesSettings): ReaderSettings {
        val next = change(seriesSettings[key] ?: SeriesSettings())
        return copy(seriesSettings = if (next == SeriesSettings()) seriesSettings - key else seriesSettings + (key to next))
    }

    /** The JSON that [parse] reads. */
    fun encode(): String = buildJsonObject {
        put("longstripWidth", longstripWidth)
        put("fit", fit.name.lowercase())
        put("spread", spread.name.lowercase())
        put("zoomWide", zoomWide)
        put("invertRtlControls", invertRtlControls)
        putJsonObject("seriesSettings") {
            for ((key, entry) in seriesSettings) {
                putJsonObject(key) {
                    put("mode", entry.mode?.name?.lowercase())
                    put("direction", entry.direction?.name?.lowercase())
                }
            }
        }
        putJsonObject("shiftedBooks") { for (id in shiftedBooks) put(id, true) }
    }.toString()

    companion object {
        /** Reads each field on its own, so one stale or out-of-range value doesn't discard the rest. */
        fun parse(json: String?): ReaderSettings {
            val stored = json?.let { runCatching { Json.parseToJsonElement(it) }.getOrNull() } as? JsonObject
                ?: return ReaderSettings()
            val defaults = ReaderSettings()
            return ReaderSettings(
                longstripWidth = stored["longstripWidth"].numberOrNull()?.takeIf { it in 10.0..100.0 }?.toInt() ?: defaults.longstripWidth,
                fit = stored["fit"].enum<Fit>() ?: defaults.fit,
                spread = stored["spread"].enum<SpreadSetting>() ?: defaults.spread,
                zoomWide = stored["zoomWide"].boolean() ?: defaults.zoomWide,
                invertRtlControls = stored["invertRtlControls"].boolean() ?: defaults.invertRtlControls,
                seriesSettings = stored["seriesSettings"].record { entry ->
                    (entry as? JsonObject)?.let { SeriesSettings(it["mode"].enum<ReaderMode>(), it["direction"].enum<ReadingDirection>()) }
                }.orEmpty(),
                shiftedBooks = stored["shiftedBooks"].record { it.boolean()?.takeIf { shifted -> shifted } }?.keys.orEmpty(),
            )
        }

        private fun JsonElement?.boolean() = (this as? JsonPrimitive)?.takeIf { !it.isString }?.booleanOrNull

        /** The web stores enum values in lower case. */
        private inline fun <reified T : Enum<T>> JsonElement?.enum(): T? {
            val name = (this as? JsonPrimitive)?.takeIf { it.isString }?.content
            return enumValues<T>().find { it.name.lowercase() == name }
        }

        /** Null when this isn't an object or one of its values doesn't read. */
        private fun <T : Any> JsonElement?.record(read: (JsonElement) -> T?): Map<String, T>? {
            val entries = (this as? JsonObject)?.mapValues { read(it.value) } ?: return null
            @Suppress("UNCHECKED_CAST")
            return if (entries.values.any { it == null }) null else entries as Map<String, T>
        }
    }
}

/** Tap zones and swipes are mirrored. Never in longstrip. */
fun controlsFlipped(mode: ReaderMode, direction: ReadingDirection, settings: ReaderSettings): Boolean =
    mode == ReaderMode.Paged && direction == ReadingDirection.Rtl && settings.invertRtlControls

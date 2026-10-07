package me.tijlvdb.voltis.domain.catalog

import java.time.Instant
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContinueReason
import me.tijlvdb.voltis.data.api.ContinueTarget
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.domain.reading.EffectiveReading
import me.tijlvdb.voltis.domain.reading.Recency

/** The volume Continue opens: its target, or for "earlier unread" the earlier volume's page. Offline it needs a copy on this device. */
val ContinueTarget.opens: String? get() = target?.id ?: earlierUnreadId?.takeIf { reason == ContinueReason.EARLIER_UNREAD }

/** A cached volume and what the phone knows of its reading ([EffectiveReading]). */
class VolumeReading(val content: Content, val reading: EffectiveReading)

/**
 * What continuing the series [seriesId] opens without the server, from its [volumes] in order, with
 * their effective reading: the backend's `resolveContinue` for a series, in its order. The
 * anchor is the volume read or set last; the target is the first one that can be read, from the
 * anchor on. Without one, the reason says why, and a held volume is the target when there is one.
 */
fun offlineContinue(seriesId: String, volumes: List<VolumeReading>): ContinueTarget {
    fun VolumeReading.status() = reading.status
    fun VolumeReading.eligible() = status().let { it == null || it == ReadingStatus.READING || it == ReadingStatus.PLAN_TO_READ }
    fun VolumeReading.hasPosition() = reading.progress.keys.any { it != "at_end" }
    fun VolumeReading.read() = reading.lastReadAt != null || status() in setOf(ReadingStatus.READING, ReadingStatus.COMPLETED, ReadingStatus.ON_HOLD, ReadingStatus.DROPPED)
    // No time at all is the oldest, as the backend's COALESCE.
    fun VolumeReading.recency() = reading.recency ?: Recency.Server(Instant.EPOCH)
    fun VolumeReading.done() = status() == ReadingStatus.COMPLETED || status() == ReadingStatus.DROPPED

    val anchor = volumes.indices.filter { volumes[it].read() }.maxWithOrNull(compareBy<Int> { volumes[it].recency() }.thenBy { it })
    fun after(i: Int) = anchor != null && i > anchor
    val earlier = anchor?.let { a -> volumes.withIndex().firstOrNull { (i, v) -> i < a && v.eligible() }?.value?.content?.id }
    fun result(target: VolumeReading?, action: String?, reason: String? = null) =
        ContinueTarget(target = target?.content, seriesId = seriesId, action = action, reason = reason, earlierUnreadId = earlier)

    val target = volumes.withIndex().firstOrNull { (i, v) ->
        v.eligible() && (anchor == null || if (volumes[anchor].eligible()) i == anchor else i > anchor)
    }?.value
    if (target != null) {
        return result(target, if (target.hasPosition()) "resume" else if (anchor == null || target === volumes[anchor]) "start" else "next")
    }
    if (volumes.isEmpty()) return result(null, null, ContinueReason.EMPTY)
    if (volumes.all { it.done() }) return result(null, null, ContinueReason.CAUGHT_UP)
    if (earlier != null) return result(null, null, ContinueReason.EARLIER_UNREAD)
    // A held volume: the first after the anchor, else the first.
    val held = volumes.withIndex().filter { it.value.status() == ReadingStatus.ON_HOLD }
        .sortedWith(compareByDescending<IndexedValue<VolumeReading>> { after(it.index) }.thenBy { it.index }).firstOrNull()?.value
    return if (held != null) result(held, if (held.hasPosition()) "resume" else "start", ContinueReason.HELD) else result(null, null)
}

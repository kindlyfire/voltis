package me.tijlvdb.voltis.data.downloads

import kotlin.reflect.KClass
import kotlinx.coroutines.runBlocking
import okio.Buffer
import okio.BufferedSource
import org.junit.Assert.assertEquals
import org.junit.Test

class OfflineStreamTest {
    private fun manifest(from: Int = 0, format: Int = 1, pageCount: Int = 3) = Buffer().apply {
        val pages = List(3) { """{"name": "00$it.jpg", "media_type": "image/jpeg", "width": 800, "height": ${if (it == 2) "null" else "1200"}}""" }
        val json = """{"format": $format, "content_id": "c_1", "version": "v1", "file_size": 3000, "file_mtime": "2026-10-04T15:12:00Z",
            "page_count": $pageCount, "from": $from, "pages": [${pages.joinToString()}], "added_later": true}"""
        val bytes = json.encodeToByteArray()
        writeInt(bytes.size)
        write(bytes)
    }

    private fun Buffer.page(index: Int, length: Int = 4, bytes: Int = length) = apply {
        writeInt(index)
        writeInt(length)
        write(ByteArray(bytes) { index.toByte() })
    }

    private fun Buffer.end() = apply {
        writeInt(-1)
        writeInt(0)
    }

    private fun Buffer.error(message: String) = apply {
        writeInt(0xFFFFFFFE.toInt())
        writeInt(message.length)
        writeUtf8(message)
    }

    /** The pages read, in order, each checked to hold its own index; and how the stream ended. */
    private fun read(stream: Buffer, from: Int = 0): Pair<List<Int>, Throwable?> = runBlocking {
        val pages = mutableListOf<Int>()
        val error = runCatching {
            readOfflineStream(stream, from, object : OfflineSink {
                override suspend fun manifest(manifest: OfflineManifest): Boolean {
                    assertEquals(3, manifest.pages.size)
                    assertEquals(null, manifest.pages[2].height)
                    return true
                }

                override suspend fun page(index: Int, length: Long, data: BufferedSource) {
                    assertEquals(List(length.toInt()) { index.toByte() }, data.readByteArray(length).toList())
                    pages += index
                }
            })
        }.exceptionOrNull()
        pages to error
    }

    @Test
    fun streams() {
        data class Case(val name: String, val stream: Buffer, val pages: List<Int>, val error: KClass<out Throwable>?, val from: Int = 0)
        val tooBig = Buffer().apply { writeInt((4 shl 20) + 1) }
        for (case in listOf(
            Case("full", manifest().page(0).page(1).page(2).end(), listOf(0, 1, 2), null),
            Case("from", manifest(from = 1).page(1).page(2).end(), listOf(1, 2), null, from = 1),
            Case("error frame", manifest().page(0).error("page 1: file not found in archive"), listOf(0), OfflineError.Page::class),
            Case("cut in a header", manifest().page(0).apply { writeShort(1) }, listOf(0), OfflineError.Truncated::class),
            Case("cut in a page", manifest().page(0).page(1, length = 10, bytes = 4), listOf(0), OfflineError.Truncated::class),
            Case("oversized manifest", tooBig, emptyList(), OfflineError.Protocol::class),
            // Lengths are unsigned: 0x80000000 is 2 GiB, not negative.
            Case("oversized page", manifest().apply { writeInt(0); writeInt(Int.MIN_VALUE) }, emptyList(), OfflineError.Protocol::class),
            Case("out of order", manifest().page(1), emptyList(), OfflineError.Protocol::class),
            Case("early end", manifest().page(0).end(), listOf(0), OfflineError.Protocol::class),
            Case("after the end", manifest().page(0).page(1).page(2).end().apply { writeByte(0) }, listOf(0, 1, 2), OfflineError.Protocol::class),
            Case("format", manifest(format = 2).page(0), emptyList(), OfflineError.Protocol::class),
            Case("another start", manifest(from = 0).page(0), emptyList(), OfflineError.Protocol::class, from = 1),
            Case("page count", manifest(pageCount = 4).page(0), emptyList(), OfflineError.Protocol::class),
        )) {
            val (pages, error) = read(case.stream, case.from)
            assertEquals(case.name, case.pages, pages)
            assertEquals(case.name, case.error, error?.let { it::class })
        }
        assertEquals("page 1: file not found in archive", read(manifest().error("page 1: file not found in archive")).second?.message)
    }
}

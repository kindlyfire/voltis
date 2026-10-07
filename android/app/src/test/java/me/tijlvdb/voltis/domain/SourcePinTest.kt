package me.tijlvdb.voltis.domain

import java.io.File
import java.security.MessageDigest
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The comic layout and the reading engine are hand-kept ports of web files (PagedLayout.kt and
 * Direction.kt; ReadingEngine, which ports readingSync.ts, see P2 section 5's mapping table). When
 * this fails, the web file changed: compare the port and its test cases with it, then record the new hash.
 */
class SourcePinTest {
    private val pins = mapOf(
        "pages/read/ComicDisplay/pagedLayout.ts" to "26ebcf5e85686a060c7b0c453bce774f8e7632d120558f0422fc14d87496de6a",
        "pages/read/ComicDisplay/direction.ts" to "11a5d9d2cf9f8c5d8ba71dbd28e357c00db77497d8dbb4217f8d2d72d1cdb8bd",
        "pages/read/readingSync.ts" to "dc9d8d6f04dd59f654ce08ee820142f180dfb6a49b7c6cc2766d20735be50b1d",
    )

    @Test
    fun portedSourcesAreUnchanged() {
        for ((name, pin) in pins) {
            val bytes = File("../../frontend/src/$name").readBytes()
            val hash = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
            assertEquals(name, pin, hash)
        }
    }
}

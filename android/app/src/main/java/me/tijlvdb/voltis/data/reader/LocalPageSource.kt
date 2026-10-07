package me.tijlvdb.voltis.data.reader

import java.io.File
import java.io.FileNotFoundException
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.downloads.CopyPin
import me.tijlvdb.voltis.data.downloads.OfflineManifest
import me.tijlvdb.voltis.domain.comic.PageDimensions
import me.tijlvdb.voltis.domain.comic.PageSource
import me.tijlvdb.voltis.domain.comic.PageUnsupported
import me.tijlvdb.voltis.domain.downloads.PageDamaged
import me.tijlvdb.voltis.domain.storage.StorageFullException

/**
 * A downloaded item's pages (P2 §9): one file per page in the pinned copy's directory, which is
 * never written again, named by its index and sized by the copy's [manifest]. The files stay
 * while [pin] is held.
 */
class LocalPageSource(
    private val pin: CopyPin,
    manifest: OfflineManifest,
    /** The copy's files failed to read back (once per source): the owner marks the copy damaged. */
    private val onDamaged: () -> Unit = {},
) : PageSource {
    private val reported = AtomicBoolean()

    override val pages = manifest.pages.map { PageDimensions(it.width ?: 0, it.height ?: 0) }

    override fun page(index: Int) = File(pin.dir, index.toString())

    override suspend fun preload(index: Int) = Unit

    override fun hold() = pin.hold()

    /**
     * A decode failure or a missing file of a copy still pinned is damage; an out-of-memory error, a
     * cancellation, a full disk or a released pin says nothing about the files.
     */
    override fun failed(index: Int, error: Throwable): Throwable {
        if (error is OutOfMemoryError || error is CancellationException || error is StorageFullException || error is PageDamaged || error is PageUnsupported) return error
        // Held for the check only: a pin released by now (the reader closed) is not this copy's fault.
        val held = pin.hold() ?: return error
        held.close()
        if (reported.compareAndSet(false, true)) onDamaged()
        return PageDamaged(error)
    }

    override suspend fun <T> read(index: Int, block: (File) -> T): T {
        val held = pin.hold() ?: throw FileNotFoundException("Page $index isn't on this device")
        return try {
            withContext(Dispatchers.IO) { block(page(index)) }
        } finally {
            held.close()
        }
    }
}

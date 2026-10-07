package me.tijlvdb.voltis.data.downloads

import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.ServerFile
import me.tijlvdb.voltis.domain.downloads.Stale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The download row's transitions, one table (start row × event → row or Delete), each result keeping I1 to I3. */
class DownloadTransitionsTest {
    private data class Case(val name: String, val row: DownloadEntity?, val event: Event, val expected: Change)

    private val m1 = "2026-01-10T06:00:00Z"
    private val m2 = "2026-02-10T06:00:00Z"
    private val m3 = "2026-03-10T06:00:00Z"

    private val queued = DownloadEntity("lantern-1", "lantern", DownloadState.QUEUED, requestedBy = RequestedBy.USER, queuedAt = 1, transferId = "t1")
    private val running = queued.copy(state = DownloadState.RUNNING, version = "v1", pageCount = 3, pagesDone = 1, bytesDone = 10, fileSize = 100, fileMtime = m1)

    /** A done copy of the file at [m1], seen as it is. */
    private val copy = queued.copy(
        state = DownloadState.DONE, transferId = null, copyId = "c1", copyVersion = "v1", copyPageCount = 3, copyBytes = 30,
        copyFileSize = 100, copyFileMtime = m1, completedAt = 5, seenAt = 60, seenMtime = m1, seenSize = 100,
    )

    /** The file changed on the server: seen at [m2]. */
    private val stale = copy.copy(seenMtime = m2, seenSize = 200, stale = Stale.VERSION)

    /** Its replacement, all pages in, from a manifest of [m2]. */
    private val replacing = stale.copy(
        state = DownloadState.RUNNING, transferId = "t2", version = "v2", pageCount = 3, pagesDone = 3, bytesDone = 40, fileSize = 200, fileMtime = m2,
    )

    private val made = CopyMade(45, cover = true, seriesCover = false)

    /** A reader found the files of its copy damaged. */
    private val damaged = copy.copy(damagedCopy = "c1", stale = Stale.DAMAGED)

    private fun write(row: DownloadEntity) = Change.Write(row)

    private val cases = listOf(
        Case("enqueue", null, Event.Enqueue(NewDownload("lantern-1", "lantern", RequestedBy.USER)), write(queued.copy(transferId = "new", queuedAt = NOW))),
        Case("enqueue an existing row", copy, Event.Enqueue(NewDownload("lantern-1", "lantern", RequestedBy.USER)), Change.None),
        Case("pause its transfer", running, Event.Pause("t1"), write(running.copy(state = DownloadState.PAUSED))),
        Case("pause another transfer", running, Event.Pause("t0"), Change.None),
        Case("cancel another transfer", running, Event.Cancel("t0"), Change.None),
        Case("cancel without a copy", running, Event.Cancel(), Change.Delete),
        Case("cancel a replacement: back to the copy", replacing.copy(error = "x", errorKind = ErrorKind.SERVER), Event.Cancel("t2"), write(stale)),
        Case("cancel a done row", copy, Event.Cancel(), Change.None),
        Case(
            "again keeps the copy and the observation", stale, Event.Again,
            write(stale.copy(state = DownloadState.QUEUED, transferId = "new", queuedAt = NOW)),
        ),
        Case("again on a current copy", copy, Event.Again, Change.None),
        Case(
            "gone with a copy: back to it, marked", replacing, RunEnd.Gone(at = 70),
            write(copy.copy(seenAt = 70, seenGone = true, seenMtime = null, seenSize = null, stale = Stale.GONE)),
        ),
        Case(
            "gone without a copy fails, on a new transfer", running, RunEnd.Gone(at = 70),
            write(
                queued.copy(
                    state = DownloadState.FAILED, transferId = "new", error = DownloadError.GONE, errorKind = ErrorKind.SERVER,
                    seenAt = 70, seenGone = true,
                ),
            ),
        ),
        Case("gone seen before the stored observation", replacing, RunEnd.Gone(at = 50), Change.None),
        Case("an older observation", stale, Event.Observe(ServerFile(true, m1, 100), at = 50), Change.None),
        Case("a newer observation clears the mark", stale, Event.Observe(ServerFile(true, m1, 100), at = 70), write(copy.copy(seenAt = 70))),
        Case(
            "an observation without a copy is only stored", queued, Event.Observe(ServerFile(true, m1, 100), at = 70),
            write(queued.copy(seenAt = 70, seenMtime = m1, seenSize = 100)),
        ),
        Case(
            "published from a manifest newer than the observation", replacing, Event.Published(made, manifestAt = 90, ServerFile(true, m2, 200)),
            write(
                stale.copy(
                    copyId = "t2", copyVersion = "v2", copyBytes = 45, copyFileSize = 200, copyFileMtime = m2, copyCover = true, completedAt = NOW,
                    seenAt = 90, stale = null,
                ),
            ),
        ),
        Case(
            "published, an observation newer than the manifest kept", replacing.copy(seenAt = 80, seenMtime = m3),
            Event.Published(made, manifestAt = 70, ServerFile(true, m2, 200)),
            write(
                stale.copy(
                    copyId = "t2", copyVersion = "v2", copyBytes = 45, copyFileSize = 200, copyFileMtime = m2, copyCover = true, completedAt = NOW,
                    seenAt = 80, seenMtime = m3, stale = Stale.VERSION,
                ),
            ),
        ),
        Case("published before the last page", replacing.copy(pagesDone = 2), Event.Published(made, 90, null), Change.None),
        Case("a page out of order", running, Event.Page(2, 10), Change.None),
        Case("broken off at 4", running.copy(attempts = 3), RunEnd.BrokenOff(stored = false), write(running.copy(state = DownloadState.QUEUED, attempts = 4, error = DownloadError.UNREACHABLE, errorKind = ErrorKind.NETWORK))),
        Case("broken off at 5", running.copy(attempts = 4), RunEnd.BrokenOff(stored = false), write(running.copy(state = DownloadState.FAILED, attempts = 5, error = DownloadError.BROKEN_OFF, errorKind = ErrorKind.NETWORK))),
        Case("a server error at 4", running.copy(attempts = 3), RunEnd.ServerError("down"), write(running.copy(state = DownloadState.QUEUED, attempts = 4, queuedAt = NOW, error = "down", errorKind = ErrorKind.SERVER))),
        Case("a server error at 5", running.copy(attempts = 4), RunEnd.ServerError("down"), write(running.copy(state = DownloadState.FAILED, attempts = 5, error = "down", errorKind = ErrorKind.SERVER))),
        Case(
            "retry keeps the prefix", running.copy(state = DownloadState.FAILED, attempts = 5, error = "down", errorKind = ErrorKind.SERVER), Event.Retry,
            write(running.copy(state = DownloadState.QUEUED)),
        ),
        Case(
            "resume all retries a full disk", running.copy(state = DownloadState.FAILED, error = DownloadError.NO_SPACE, errorKind = ErrorKind.STORAGE), Event.ResumeAll,
            write(running.copy(state = DownloadState.QUEUED)),
        ),
        Case(
            "recover a copy with a missing page", copy, Event.Recover(copyValid = false),
            write(
                queued.copy(
                    state = DownloadState.FAILED, transferId = "new", error = DownloadError.MISSING_FILES, errorKind = ErrorKind.STORAGE,
                    seenAt = 60, seenMtime = m1, seenSize = 100,
                ),
            ),
        ),
        Case("recover a running row", running, Event.Recover(copyValid = true), write(running.copy(state = DownloadState.QUEUED))),
        Case("delete", stale, Event.Delete, Change.Delete),
        Case("damaged copy", copy, Event.Damaged("c1"), write(damaged)),
        Case("damaged: a former copy marks nothing", copy, Event.Damaged("c0"), Change.None),
        Case("damaged: marked already", damaged, Event.Damaged("c1"), Change.None),
        Case("damaged survives an observation that matches", damaged, Event.Observe(ServerFile(true, m1, 100), at = 70), write(damaged.copy(seenAt = 70))),
        Case(
            "damaged outranks a new version", damaged, Event.Observe(ServerFile(true, m2, 200), at = 70),
            write(damaged.copy(seenAt = 70, seenMtime = m2, seenSize = 200)),
        ),
        Case(
            "gone outranks damaged", damaged, RunEnd.Gone(at = 70),
            write(damaged.copy(seenAt = 70, seenGone = true, seenMtime = null, seenSize = null, stale = Stale.GONE)),
        ),
        Case("again from damaged: a new transfer, the copy kept", damaged, Event.Again, write(damaged.copy(state = DownloadState.QUEUED, transferId = "new", queuedAt = NOW))),
        Case(
            "published clears damaged", damaged.copy(state = DownloadState.RUNNING, transferId = "t2", version = "v2", pageCount = 3, pagesDone = 3, bytesDone = 40, fileSize = 200, fileMtime = m2),
            Event.Published(made, manifestAt = 90, ServerFile(true, m2, 200)),
            write(
                copy.copy(
                    copyId = "t2", copyVersion = "v2", copyBytes = 45, copyFileSize = 200, copyFileMtime = m2, copyCover = true, completedAt = NOW,
                    seenAt = 90, seenMtime = m2, seenSize = 200,
                ),
            ),
        ),
    )

    @Test
    fun transitions() {
        for (case in cases) {
            val change = transition(case.row, case.event, NOW) { "new" }
            assertEquals(case.name, case.expected, change)
            val row = (change as? Change.Write)?.row ?: continue
            assertTrue("${case.name}: I1", (row.state == DownloadState.DONE) == (row.transferId == null))
            assertTrue("${case.name}: I2", row.copyId != null || row.transferId != null)
            assertTrue("${case.name}: I3", row.stale == null || row.copyId != null)
        }
    }

    private companion object {
        const val NOW = 100L
    }
}

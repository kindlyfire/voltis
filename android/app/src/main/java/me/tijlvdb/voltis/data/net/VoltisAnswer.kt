package me.tijlvdb.voltis.data.net

import java.io.IOException
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AppJson
import okhttp3.Response
import retrofit2.HttpException

/**
 * Whether a response came from Voltis (P2 §10). A portal, a proxy or another device at the address
 * must not sign the user out or drop anything, so only a genuine answer counts.
 */
sealed interface VoltisAnswer {
    /** [message] is Voltis' error message, for an error status. */
    data class Genuine(val code: Int, val message: String? = null) : VoltisAnswer

    data object Unreachable : VoltisAnswer

    companion object {
        /**
         * A 2xx is genuine when the caller could read it as what it asked for: a 2xx body that doesn't
         * parse is [of] a `SerializationException`. An error status is genuine only with Voltis' body.
         */
        fun of(code: Int, errorBody: String?, websocket: Boolean = false): VoltisAnswer = when {
            code == 101 -> if (websocket) Genuine(code) else Unreachable
            code in 200..299 -> Genuine(code)
            code in 502..504 -> Unreachable
            else -> errorMessage(code, errorBody)?.let { Genuine(code, it) } ?: Unreachable
        }

        /**
         * Before anything parsed the body: a 2xx counts by its content type (JSON, the offline stream,
         * an image), so a portal's HTML page doesn't. A 101 counts only for a request that asked to
         * upgrade to a WebSocket. Reads at most 64 KiB of an error body, leaving the response unconsumed.
         */
        fun of(response: Response): VoltisAnswer {
            if (response.code == 101) return of(101, null, response.request.header("Upgrade").equals("websocket", ignoreCase = true))
            if (response.isSuccessful) {
                val type = response.body.contentType()
                val known = response.code == 204 || type?.subtype == "json" || type?.type == "image" ||
                    type?.subtype == "vnd.voltis.pages"
                return if (known) Genuine(response.code) else Unreachable
            }
            return of(response.code, response.peekBody(64 * 1024L).string())
        }

        /** `{"error": …}`, or a reading 409's `{message, state, …}`. */
        private fun errorMessage(code: Int, body: String?): String? {
            val json = body?.let { runCatching { AppJson.parseToJsonElement(it) }.getOrNull() } as? JsonObject ?: return null
            val error = json["error"] as? JsonPrimitive
            if (error != null && error.isString) return error.content
            val message = json["message"] as? JsonPrimitive
            return message?.content?.takeIf { code == 409 && message.isString && json["state"] is JsonObject }
        }
    }
}

/**
 * The 404 messages by which Voltis says the content itself is missing (`backend/routes/reading.go`,
 * `content.go` `getContent`, and `files.go` `offline` for a file gone from disk). Any other 404 (a
 * route Echo doesn't know, a proxy's `{"error": "Not Found"}`) proves nothing about the content.
 */
object Missing {
    const val CONTENT = "Content not found"

    /** `series-reading`'s `until_id`. */
    const val VOLUME = "Volume not found"
    const val FILE = "File not found"
}

/** Voltis' own 404 for content that isn't there (any other 404 says nothing about it). */
fun Throwable.isContentMissing(): Boolean =
    this is HttpException && code() == 404 && VoltisAnswer.of(code(), errorBodyPeek()) == VoltisAnswer.Genuine(404, Missing.CONTENT)

// Retrofit buffers an error body, so peeking leaves it for whoever reads the message.
private fun HttpException.errorBodyPeek(): String? = response()?.errorBody()?.let { runCatching { it.source().peek().readUtf8() }.getOrNull() }

/** A failed API call that Voltis didn't answer (P2 §10): no response, a body that didn't parse, or an error status without Voltis' body. */
fun Throwable.isUnreachable(): Boolean = when (this) {
    is HttpException -> VoltisAnswer.of(code(), errorBodyPeek()) == VoltisAnswer.Unreachable
    is IOException, is SerializationException -> true
    else -> false
}

package me.tijlvdb.voltis.data.content

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.PLACEHOLDER_HOST
import okhttp3.HttpUrl

// Image URLs are on the placeholder host; the interceptors add the server and the bearer.

private fun files() = HttpUrl.Builder().scheme("http").host(PLACEHOLDER_HOST).addPathSegments("api/files")

fun pageUrl(content: Content, index: Int): HttpUrl = files()
    .addPathSegment("comic-page")
    .addPathSegment(content.id)
    .addPathSegment(index.toString())
    // Makes the response immutable. The builder encodes the `+` of the timestamp.
    .addQueryParameter("v", content.fileMtime.orEmpty())
    .build()

/** Null without a cover version: the cover then shows its placeholder, as on the web. */
fun coverUrl(content: Content): String? = coverUrl(content.id, content.coverVersion)

fun coverUrl(id: String, version: String?): String? = version?.let {
    files().addPathSegment("cover").addPathSegment(id).addQueryParameter("v", it).build().toString()
}

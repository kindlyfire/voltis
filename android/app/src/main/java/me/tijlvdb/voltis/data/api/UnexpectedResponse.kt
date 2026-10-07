package me.tijlvdb.voltis.data.api

import java.lang.reflect.Type
import kotlinx.serialization.SerializationException
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody
import retrofit2.Converter
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * A 2xx whose body is valid JSON that doesn't fit the app's DTO. It proves nothing about the server (a portal
 * or proxy can send `{}`), so it is a [SerializationException] like any body that doesn't parse: unreachable for every
 * fallback and sync decision, and for connectivity, which only probes. It changes what the user is told.
 */
class UnexpectedResponse(cause: Throwable) : SerializationException("The server sent something this app can't read", cause)

/** [AppJson]'s Retrofit converter, with a decode failure on a syntactically valid JSON body rethrown as [UnexpectedResponse]. */
fun appConverterFactory(): Converter.Factory =
    ClassifyingConverterFactory(AppJson.asConverterFactory("application/json".toMediaType()))

private class ClassifyingConverterFactory(private val delegate: Converter.Factory) : Converter.Factory() {
    override fun responseBodyConverter(type: Type, annotations: Array<Annotation>, retrofit: Retrofit): Converter<ResponseBody, *>? {
        val inner = delegate.responseBodyConverter(type, annotations, retrofit) ?: return null
        return Converter<ResponseBody, Any?> { body ->
            val bytes = body.use { it.bytes() }
            try {
                inner.convert(ResponseBody.create(body.contentType(), bytes))
            } catch (e: SerializationException) {
                // HTML, a truncated body or an empty one stays a plain SerializationException.
                val json = runCatching { AppJson.parseToJsonElement(bytes.decodeToString()) }.isSuccess
                if (json) throw UnexpectedResponse(e) else throw e
            }
        }
    }

    override fun requestBodyConverter(
        type: Type,
        parameterAnnotations: Array<Annotation>,
        methodAnnotations: Array<Annotation>,
        retrofit: Retrofit,
    ) = delegate.requestBodyConverter(type, parameterAnnotations, methodAnnotations, retrofit)

    override fun stringConverter(type: Type, annotations: Array<Annotation>, retrofit: Retrofit) =
        delegate.stringConverter(type, annotations, retrofit)
}

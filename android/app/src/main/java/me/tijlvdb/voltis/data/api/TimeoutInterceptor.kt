package me.tijlvdb.voltis.data.api

import java.util.concurrent.TimeUnit
import okhttp3.Interceptor
import okhttp3.Response

/** `X-Read-Timeout: <seconds>` and `X-Connect-Timeout: <seconds>` set those timeouts for one request; the headers aren't sent. */
class TimeoutInterceptor : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        var next = chain
        request.header(HEADER)?.let { next = next.withReadTimeout(it.toInt(), TimeUnit.SECONDS) }
        request.header(CONNECT_HEADER)?.let { next = next.withConnectTimeout(it.toInt(), TimeUnit.SECONDS) }
        if (next === chain) return chain.proceed(request)
        return next.proceed(request.newBuilder().removeHeader(HEADER).removeHeader(CONNECT_HEADER).build())
    }

    companion object {
        const val HEADER = "X-Read-Timeout"
        const val CONNECT_HEADER = "X-Connect-Timeout"
    }
}

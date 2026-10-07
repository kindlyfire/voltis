package me.tijlvdb.voltis.data.api

import android.content.Context
import dagger.Lazy
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.io.IOException
import javax.inject.Singleton
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.net.ReachabilityInterceptor
import me.tijlvdb.voltis.domain.net.Connectivity
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.RequestBody
import okhttp3.Response
import okio.BufferedSink
import retrofit2.Retrofit

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {
    @Provides
    @Singleton
    fun okHttp(@ApplicationContext context: Context, store: SessionStore, connectivity: Lazy<Connectivity>): OkHttpClient = OkHttpClient.Builder()
        .addInterceptor(SendOnceInterceptor())
        .addInterceptor(TimeoutInterceptor())
        // Order matters: the others must see the rewritten host.
        .addInterceptor(ServerUrlInterceptor(store))
        // Lazy: Connectivity probes through this client.
        .addInterceptor(ReachabilityInterceptor(store) { connectivity.get() })
        .addInterceptor(AuthInterceptor(store) { expected -> connectivity.get().probe() && store.active()?.server?.id == expected.id })
        .addInterceptor { chain ->
            // Only relabels a failure: a local address over a VPN works without the permission.
            try {
                chain.proceed(chain.request())
            } catch (e: IOException) {
                throw if (context.lacksLocalNetwork(chain.request().url)) LocalNetworkException() else e
            }
        }
        .build()

    @Provides
    @Singleton
    fun api(client: OkHttpClient): VoltisApi = Retrofit.Builder()
        .baseUrl("http://$PLACEHOLDER_HOST/")
        .client(client)
        .addConverterFactory(appConverterFactory())
        .build()
        .create(VoltisApi::class.java)
}

/** A request's tag: it is sent at most once. For a write whose lost answer must not be followed by a resend: a list's create and reorder. */
object SendOnce

/**
 * Makes a [SendOnce] request's body one-shot, so OkHttp neither retries it once sending began nor
 * follows a redirect that would send it again. Retries before anything was sent (connecting) stay.
 */
class SendOnceInterceptor : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val body = request.body
        if (request.tag(SendOnce::class.java) == null || body == null) return chain.proceed(request)
        return chain.proceed(request.newBuilder().method(request.method, OneShot(body)).build())
    }

    private class OneShot(private val body: RequestBody) : RequestBody() {
        override fun contentType() = body.contentType()

        override fun contentLength() = body.contentLength()

        override fun writeTo(sink: BufferedSink) = body.writeTo(sink)

        override fun isOneShot() = true
    }
}

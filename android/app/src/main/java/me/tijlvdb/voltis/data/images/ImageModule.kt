package me.tijlvdb.voltis.data.images

import android.content.Context
import coil3.ImageLoader
import coil3.annotation.ExperimentalCoilApi
import coil3.disk.DiskCache
import coil3.network.cachecontrol.CacheControlCacheStrategy
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton
import okhttp3.OkHttpClient
import okio.Path.Companion.toOkioPath

@Module
@InstallIn(SingletonComponent::class)
object ImageModule {
    /**
     * Images go through the app's OkHttpClient, so Coil never sees the token or the real host.
     * The cache-control strategy keeps a `no-store` fallback cover out of the disk cache, where
     * it would otherwise be pinned under the versioned URL.
     */
    @OptIn(ExperimentalCoilApi::class)
    @Provides
    @Singleton
    fun imageLoader(
        @ApplicationContext context: Context,
        client: OkHttpClient,
        serverKey: ServerKeyInterceptor,
    ): ImageLoader = ImageLoader.Builder(context)
        .components {
            add(serverKey)
            add(OkHttpNetworkFetcherFactory(callFactory = { client }, cacheStrategy = { CacheControlCacheStrategy() }))
        }
        .diskCache { DiskCache.Builder().directory(context.cacheDir.resolve("images").toOkioPath()).build() }
        .build()
}

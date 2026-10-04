package me.tijlvdb.voltis.data.api

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.io.IOException
import javax.inject.Singleton
import me.tijlvdb.voltis.data.auth.SessionStore
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {
    @Provides
    @Singleton
    fun okHttp(@ApplicationContext context: Context, store: SessionStore): OkHttpClient = OkHttpClient.Builder()
        // Order matters: the others must see the rewritten host.
        .addInterceptor(ServerUrlInterceptor(store))
        .addInterceptor(AuthInterceptor(store))
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
        .addConverterFactory(AppJson.asConverterFactory("application/json".toMediaType()))
        .build()
        .create(VoltisApi::class.java)
}

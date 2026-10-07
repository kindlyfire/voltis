package me.tijlvdb.voltis.data.api

import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import retrofit2.Retrofit

/** The API as the app builds it, at [baseUrl]. The default is for a fake that delegates what it doesn't answer. */
fun testApi(baseUrl: HttpUrl = "http://voltis.invalid/".toHttpUrl(), client: OkHttpClient = OkHttpClient()): VoltisApi = Retrofit.Builder()
    .baseUrl(baseUrl)
    .client(client)
    .addConverterFactory(appConverterFactory())
    .build()
    .create(VoltisApi::class.java)

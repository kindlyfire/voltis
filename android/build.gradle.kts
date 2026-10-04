plugins {
    alias(libs.plugins.android.application) apply false
    // Not applied (AGP has built-in Kotlin); declared to pick the Kotlin version AGP compiles with.
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.compose) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.ksp) apply false
    alias(libs.plugins.hilt) apply false
}

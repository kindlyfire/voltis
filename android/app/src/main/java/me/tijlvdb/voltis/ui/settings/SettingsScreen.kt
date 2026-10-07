package me.tijlvdb.voltis.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.SessionMethod
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.data.settings.ThemeMode
import me.tijlvdb.voltis.ui.Loaded
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VCard
import me.tijlvdb.voltis.ui.kit.VCheckboxRow
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VDisabled
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSettingChoice
import me.tijlvdb.voltis.ui.kit.VSheet
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.readableWidth
import me.tijlvdb.voltis.ui.topInset

/** The account (read-only) with its sessions, the server and Sign out, the interface settings, and About. */
@Composable
fun SettingsScreen(
    openSessions: () -> Unit,
    openReaderDefaults: () -> Unit,
    openLibraryVisibility: () -> Unit,
    openDownloadSettings: () -> Unit,
    vm: SettingsViewModel = hiltViewModel(),
) {
    RefreshOnResume(vm.refresher)
    val session by vm.session.collectAsStateWithLifecycle()
    val me by vm.me.collectAsStateWithLifecycle()
    val colors = MaterialTheme.colorScheme
    Surface(Modifier.fillMaxSize()) {
        Column(
            Modifier.topInset().verticalScroll(rememberScrollState()).readableWidth(pageGutter()).padding(horizontal = pageGutter()).padding(bottom = bottomSpace()),
            Arrangement.spacedBy(16.dp),
        ) {
            PageHeader(stringResource(R.string.tab_settings))
            OfflineBar()
            // Offline the account is the session's username; what the server keeps waits for a connection (P2 §10).
            val offline = needsConnection()
            VCard(stringResource(R.string.settings_account)) {
                val username = me?.username ?: (session as? SessionState.SignedIn)?.username.orEmpty()
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.size(40.dp).background(colors.primaryContainer, CircleShape).clearAndSetSemantics {}, Alignment.Center) {
                        Text(
                            username.take(1).uppercase(),
                            color = colors.onPrimaryContainer,
                            style = MaterialTheme.typography.labelLarge,
                        )
                    }
                    Column {
                        Text(username, style = MaterialTheme.typography.bodyLarge)
                        me?.email?.takeIf { it.isNotEmpty() }?.let { Text(it, color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodySmall) }
                    }
                }
                if (offline == null || me != null) {
                    Loaded(me, vm.error, retry = vm::load) {
                        Text(stringResource(R.string.settings_method, methodLabel(it.sessionMethod)), color = colors.onSurfaceVariant)
                    }
                }
                LinkRow(stringResource(R.string.sessions_title), openSessions, offline)
            }
            VCard(stringResource(R.string.settings_server)) {
                val url = session.server?.url
                Column {
                    Text(url?.host.orEmpty(), style = MaterialTheme.typography.bodyLarge)
                    Text(url?.toString().orEmpty(), color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                }
                VButton(stringResource(R.string.settings_sign_out), vm::askSignOut, style = VButtonStyle.Tonal, enabled = !vm.signingOut)
                if (vm.confirming) SignOutDialog(url?.host.orEmpty(), vm)
            }
            VCard(stringResource(R.string.settings_interface)) {
                val theme by vm.theme.collectAsStateWithLifecycle()
                VSettingChoice(
                    stringResource(R.string.settings_theme),
                    listOf(
                        ThemeMode.System to stringResource(R.string.theme_system),
                        ThemeMode.Light to stringResource(R.string.theme_light),
                        ThemeMode.Dark to stringResource(R.string.theme_dark),
                    ),
                    theme,
                    { it?.let(vm::setTheme) },
                )
                Column {
                    LinkRow(stringResource(R.string.visibility_title), openLibraryVisibility, offline)
                    LinkRow(stringResource(R.string.defaults_title), openReaderDefaults)
                    LinkRow(stringResource(R.string.settings_downloads), openDownloadSettings)
                }
            }
            VCard(stringResource(R.string.settings_about)) { About(vm.serverVersion) }
        }
    }
}

/**
 * Sign out of [host]. When the account left downloads or unsent changes, says they stay, with
 * "Also delete them" (off by default, P2 decision 17); else Phase 1's plain confirmation.
 */
@Composable
private fun SignOutDialog(host: String, vm: SettingsViewModel) {
    val left by vm.leftBehind.collectAsStateWithLifecycle()
    var delete by rememberSaveable { mutableStateOf(false) }
    val title = stringResource(R.string.settings_sign_out_confirm, host)
    val confirm = stringResource(R.string.settings_sign_out)
    if (!left.any) {
        VDialog(title, confirm, onConfirm = { vm.signOut(delete = false) }, onDismiss = vm::dismissSignOut)
        return
    }
    val downloads = pluralStringResource(R.plurals.sign_out_downloads, left.downloads, left.downloads)
    val changes = pluralStringResource(R.plurals.sign_out_changes, left.unsent, left.unsent)
    val what = when {
        left.unsent == 0 -> downloads
        left.downloads == 0 -> changes
        else -> stringResource(R.string.sign_out_both, downloads, changes)
    }
    val one = left.downloads + left.unsent == 1
    VDialog(
        title,
        confirm,
        onConfirm = { vm.signOut(delete) },
        onDismiss = vm::dismissSignOut,
        text = stringResource(if (one) R.string.sign_out_kept_one else R.string.sign_out_kept, what),
        danger = delete,
    ) {
        VCheckboxRow(stringResource(R.string.sign_out_delete), delete, { delete = it })
    }
}

/** A row that opens a sub-screen; disabled for a [disabled] reason. */
@Composable
private fun LinkRow(text: String, onClick: () -> Unit, disabled: String? = null) {
    VDisabled(disabled, text) { enabled ->
        Row(
            Modifier.fillMaxWidth().clickable(enabled, role = Role.Button, onClick = onClick).heightIn(min = 48.dp).alpha(if (enabled) 1f else 0.38f),
            Arrangement.SpaceBetween,
            Alignment.CenterVertically,
        ) {
            Text(text, style = MaterialTheme.typography.bodyLarge)
            Icon(VIcons.ChevronRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** The app's version, and the bundled fonts with their licenses from `assets/licenses`. */
@Composable
private fun About(serverVersion: String?) {
    val context = LocalContext.current
    val version = remember { context.packageManager.getPackageInfo(context.packageName, 0).versionName.orEmpty() }
    Text(stringResource(R.string.about_version, version))
    serverVersion?.let { Text(stringResource(R.string.about_server_version, it)) }
    Text(stringResource(R.string.about_fonts), color = MaterialTheme.colorScheme.onSurfaceVariant)
    val licenses = listOf("Figtree-OFL.txt" to R.string.about_figtree_license, "SourceSerif4-OFL.txt" to R.string.about_source_serif_license)
    var shown by rememberSaveable { mutableStateOf<String?>(null) }
    Column { for ((file, title) in licenses) LinkRow(stringResource(title), { shown = file }) }
    val (file, title) = licenses.find { it.first == shown } ?: return
    VSheet(stringResource(title), onDismiss = { shown = null }) {
        val text = remember(file) { context.assets.open("licenses/$file").bufferedReader().use { it.readText() } }
        Text(text, style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
fun methodLabel(method: String) = when (method) {
    SessionMethod.PASSWORD -> stringResource(R.string.method_password)
    SessionMethod.OIDC -> stringResource(R.string.method_oidc)
    SessionMethod.PROXY -> stringResource(R.string.method_proxy)
    else -> method
}

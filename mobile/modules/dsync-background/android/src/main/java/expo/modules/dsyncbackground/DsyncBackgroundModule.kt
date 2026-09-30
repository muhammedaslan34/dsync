package expo.modules.dsyncbackground

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

// JS side: mobile/src/background.ts. update() shows (or refreshes) the
// ongoing "Sending report.mp4" notification and keeps the app running in
// the background while it's there; stop() removes it.
class DsyncBackgroundModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("DsyncBackground")

    // progress: 0..100, or -1 when unknown. upload: arrow up (sending) or down.
    Function("update") { title: String, text: String, progress: Int, upload: Boolean ->
      val ctx = appContext.reactContext ?: return@Function false
      TransferService.update(ctx, title, text, progress, upload)
    }

    Function("stop") {
      appContext.reactContext?.let { TransferService.stop(it) }
      null
    }
  }
}

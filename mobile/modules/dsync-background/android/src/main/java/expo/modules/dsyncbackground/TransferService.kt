package expo.modules.dsyncbackground

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * A foreground service with an ongoing notification ("Sending report.mp4",
 * 45 %). While it runs, Android keeps the app's process (and so its
 * JavaScript, which does the actual transfer) alive after the user leaves
 * the app or swipes it away. It does no work of its own.
 */
class TransferService : Service() {
  companion object {
    private const val TAG = "DsyncTransfers"
    private const val CHANNEL = "dsync-transfers"
    private const val NOTIFICATION_ID = 47102

    @Volatile private var running = false

    fun update(ctx: Context, title: String, text: String, progress: Int, upload: Boolean): Boolean {
      val n = build(ctx, title, text, progress, upload)
      if (running) {
        try {
          NotificationManagerCompat.from(ctx).notify(NOTIFICATION_ID, n)
        } catch (e: SecurityException) {
          // No notification permission: the service still runs.
        }
        return true
      }
      pending = n
      return try {
        ContextCompat.startForegroundService(ctx, Intent(ctx, TransferService::class.java))
        running = true
        true
      } catch (e: Exception) {
        // Android 12+ refuses to start it while the app is in the background.
        Log.w(TAG, "could not start the transfer service", e)
        false
      }
    }

    fun stop(ctx: Context) {
      if (!running) return
      running = false
      ctx.stopService(Intent(ctx, TransferService::class.java))
    }

    @Volatile private var pending: Notification? = null

    private fun build(ctx: Context, title: String, text: String, progress: Int, upload: Boolean): Notification {
      val nm = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && nm.getNotificationChannel(CHANNEL) == null) {
        val name = ctx.applicationInfo.loadLabel(ctx.packageManager).toString()
        nm.createNotificationChannel(NotificationChannel(CHANNEL, name, NotificationManager.IMPORTANCE_LOW).apply {
          setShowBadge(false)
        })
      }
      val open = ctx.packageManager.getLaunchIntentForPackage(ctx.packageName)?.let {
        PendingIntent.getActivity(ctx, 0, it, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
      }
      return NotificationCompat.Builder(ctx, CHANNEL)
        .setSmallIcon(if (upload) android.R.drawable.stat_sys_upload else android.R.drawable.stat_sys_download)
        .setContentTitle(title)
        .setContentText(text)
        .setOngoing(true)
        .setOnlyAlertOnce(true)
        .setSilent(true)
        .setCategory(NotificationCompat.CATEGORY_PROGRESS)
        .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        .setProgress(100, progress.coerceIn(0, 100), progress < 0)
        .setContentIntent(open)
        .build()
    }
  }

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    val n = pending ?: build(this, applicationInfo.loadLabel(packageManager).toString(), "", -1, true)
    try {
      ServiceCompat.startForeground(
        this, NOTIFICATION_ID, n,
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC else 0,
      )
    } catch (e: Exception) {
      Log.w(TAG, "could not go to the foreground", e)
      running = false
      stopSelf()
    }
    return START_NOT_STICKY
  }

  // Android 15 limits data-sync services to 6 hours a day.
  override fun onTimeout(startId: Int, fgsType: Int) {
    running = false
    stopSelf()
  }

  override fun onDestroy() {
    running = false
    super.onDestroy()
  }
}

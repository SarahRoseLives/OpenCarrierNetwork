package dev.sarahsforge.ocnphone

import android.media.AudioAttributes
import android.media.AudioManager
import android.media.MediaPlayer
import android.media.ToneGenerator
import android.os.Handler
import android.os.Looper
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val handler = Handler(Looper.getMainLooper())
    private var toneGen: ToneGenerator? = null
    private var ringbackPlayer: MediaPlayer? = null
    private var previousAudioMode: Int = AudioManager.MODE_NORMAL
    private var previousSpeakerphone: Boolean = false
    private var ringbackActive = false

    private val dtmfTones = mapOf(
        "1" to ToneGenerator.TONE_DTMF_1,
        "2" to ToneGenerator.TONE_DTMF_2,
        "3" to ToneGenerator.TONE_DTMF_3,
        "4" to ToneGenerator.TONE_DTMF_4,
        "5" to ToneGenerator.TONE_DTMF_5,
        "6" to ToneGenerator.TONE_DTMF_6,
        "7" to ToneGenerator.TONE_DTMF_7,
        "8" to ToneGenerator.TONE_DTMF_8,
        "9" to ToneGenerator.TONE_DTMF_9,
        "0" to ToneGenerator.TONE_DTMF_0,
        "*" to ToneGenerator.TONE_DTMF_S,
        "#" to ToneGenerator.TONE_DTMF_P,
    )

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "ocn/dtmf")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "tone" -> {
                        val key = call.argument<String>("key") ?: ""
                        playTone(key)
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "ocn/ringback")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "start" -> {
                        startRingback()
                        result.success(null)
                    }
                    "stop" -> {
                        stopRingback()
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
    }

    /** Plays a DTMF keypad tone out of the earpiece, like a real phone. */
    private fun playTone(key: String) {
        val tone = dtmfTones[key] ?: return
        val audioManager = getSystemService(AUDIO_SERVICE) as AudioManager
        val previousMode = audioManager.mode
        val previousSpeaker = audioManager.isSpeakerphoneOn

        // Route audio to the earpiece for the duration of the tone, then
        // restore the previous audio state.
        audioManager.mode = AudioManager.MODE_IN_COMMUNICATION
        audioManager.isSpeakerphoneOn = false

        val tg = toneGen
            ?: ToneGenerator(AudioManager.STREAM_DTMF, 85).also { toneGen = it }
        tg.startTone(tone, 130)

        handler.postDelayed({
            try {
                tg.stopTone()
            } catch (_: Exception) {
                // ignore
            }
            audioManager.mode = previousMode
            audioManager.isSpeakerphoneOn = previousSpeaker
        }, 160)
    }

    /** Starts a looping ringback tone through the earpiece. */
    private fun startRingback() {
        if (ringbackActive) return
        val audioManager = getSystemService(AUDIO_SERVICE) as AudioManager
        previousAudioMode = audioManager.mode
        previousSpeakerphone = audioManager.isSpeakerphoneOn

        audioManager.mode = AudioManager.MODE_IN_COMMUNICATION
        audioManager.isSpeakerphoneOn = false

        try {
            val player = MediaPlayer()
            player.setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                    .build()
            )
            player.setDataSource(applicationContext, android.net.Uri.parse("android.resource://" + packageName + "/" + R.raw.ring))
            player.isLooping = true
            player.prepare()
            player.start()
            ringbackPlayer = player
            ringbackActive = true
        } catch (e: Exception) {
            stopRingback()
        }
    }

    /** Stops the ringback tone and restores the previous audio state. */
    private fun stopRingback() {
        if (!ringbackActive) return
        ringbackActive = false
        try {
            ringbackPlayer?.stop()
        } catch (_: Exception) {
            // ignore
        }
        try {
            ringbackPlayer?.release()
        } catch (_: Exception) {
            // ignore
        }
        ringbackPlayer = null

        try {
            val audioManager = getSystemService(AUDIO_SERVICE) as AudioManager
            audioManager.mode = previousAudioMode
            audioManager.isSpeakerphoneOn = previousSpeakerphone
        } catch (_: Exception) {
            // ignore
        }
    }
}

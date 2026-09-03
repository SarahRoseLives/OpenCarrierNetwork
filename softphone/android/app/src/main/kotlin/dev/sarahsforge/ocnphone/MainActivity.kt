package dev.sarahsforge.ocnphone

import android.media.AudioManager
import android.media.ToneGenerator
import android.os.Handler
import android.os.Looper
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private val handler = Handler(Looper.getMainLooper())
    private var toneGen: ToneGenerator? = null

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
}

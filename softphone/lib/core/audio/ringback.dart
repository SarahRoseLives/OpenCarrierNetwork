import 'dart:io';
import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/services.dart';

const MethodChannel _ringbackChannel = MethodChannel('ocn/ringback');

/// Plays the caller's ringback tone ("phone ringing" while waiting for the
/// other party to answer).
///
/// On Android this is a looping native MediaPlayer (raw/ring) routed to the
/// earpiece, like a real phone. On desktop it loops the bundled ringtone asset
/// through audioplayers.
class Ringback {
  Ringback._();

  static AudioPlayer? _player;

  static Future<void> start() async {
    try {
      if (Platform.isAndroid) {
        await _ringbackChannel.invokeMethod<void>('start');
        return;
      }
      final player = _player ??= AudioPlayer();
      await player.setReleaseMode(ReleaseMode.loop);
      await player.setVolume(1.0);
      await player.play(AssetSource('ringtone/ring.wav'));
    } catch (_) {
      // Ringback is feedback; never let it break the call flow.
    }
  }

  static Future<void> stop() async {
    try {
      if (Platform.isAndroid) {
        await _ringbackChannel.invokeMethod<void>('stop');
        return;
      }
      final player = _player;
      _player = null;
      if (player != null) {
        await player.stop();
        await player.dispose();
      }
    } catch (_) {
      // ignore
    }
  }
}

import 'dart:io';
import 'package:flutter_webrtc/flutter_webrtc.dart';

class WebRTCManager {
  final Map<String, dynamic> _defaultIce = {
    'iceServers': [
      {'urls': 'stun:stun.l.google.com:19302'},
    ],
  };

  /// Creates a PeerConnection using [iceServers] when provided (from the OCN
  /// server, which may include TURN), otherwise falls back to Google STUN.
  Future<RTCPeerConnection> createPeerConnection({
    List<Map<String, dynamic>>? iceServers,
  }) async {
    final factory = RTCFactoryNative.instance;
    final config = <String, dynamic>{
      if (iceServers != null && iceServers.isNotEmpty)
        'iceServers': iceServers
      else
        ..._defaultIce,
    };
    final pc = await factory.createPeerConnection(config, {});
    return pc;
  }

  Future<MediaStream> getLocalStream() async {
    final constraints = <String, dynamic>{'audio': true, 'video': false};
    final stream = await navigator.mediaDevices.getUserMedia(constraints);

    // Route audio through earpiece on Android by default
    if (Platform.isAndroid) {
      await Helper.setSpeakerphoneOn(false);
    }

    return stream;
  }

  Future<void> setSpeakerphone(bool enabled) async {
    if (Platform.isAndroid) {
      await Helper.setSpeakerphoneOn(enabled);
    }
  }

  Future<RTCSessionDescription> createOffer(RTCPeerConnection pc) async {
    final offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    return offer;
  }

  Future<RTCSessionDescription> createAnswer(RTCPeerConnection pc) async {
    final answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    return answer;
  }

  Future<void> setRemoteDescription(
    RTCPeerConnection pc,
    RTCSessionDescription sdp,
  ) async {
    await pc.setRemoteDescription(sdp);
  }

  Future<void> addIceCandidate(
    RTCPeerConnection pc,
    RTCIceCandidate candidate,
  ) async {
    await pc.addCandidate(candidate);
  }
}

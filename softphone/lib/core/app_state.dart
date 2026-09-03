import 'dart:developer';
import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import '../core/ksim/ksim.dart';
import '../core/notifications/call_notifications.dart';
import '../core/provision/ocn_ksim_uri.dart';
import '../core/audio/ringback.dart';
import '../core/signaling/signaling_client.dart';
import '../core/webrtc/webrtc_manager.dart';

enum AppStatus {
  uninitialized,
  needsRegistration,
  connecting,
  connected,
  reconnecting,
  error,
}

class CallSession {
  String callId;
  final String remoteNumber;
  final String remoteName;
  final bool isIncoming;
  CallState state;
  RTCPeerConnection? peerConnection;
  MediaStream? localStream;
  MediaStream? remoteStream;
  String? pendingOfferSdp;
  String? serviceCode;
  String? serviceName;
  bool remoteDescriptionSet = false;
  final List<RTCIceCandidate> pendingLocalIce = [];
  final List<RTCIceCandidate> pendingRemoteIce = [];

  CallSession({
    required this.callId,
    required this.remoteNumber,
    required this.remoteName,
    required this.isIncoming,
    this.state = CallState.idle,
  });

  bool get isService => serviceCode != null;
}

enum CallState { idle, calling, ringing, connected, ended }

class AppState extends ChangeNotifier {
  AppStatus status = AppStatus.uninitialized;
  KSimKeypair? keypair;
  OcnPhoneNumber? phoneNumber;
  String displayName = '';
  String? errorMessage;
  String? _fcmToken;

  CallSession? activeCall;
  bool isMuted = false;
  bool isSpeaker = false;
  AudioPlayer? _ringer;

  final SignalingClient _signaling;
  final WebRTCManager _webrtc = WebRTCManager();

  /// Builds the flutter_webrtc ICE server list from what the server provided
  /// (STUN/TURN from the registry), falling back to the built-in default.
  List<Map<String, dynamic>>? _iceServersForCall() {
    final servers = _signaling.iceServers;
    if (servers.isEmpty) return null;
    return servers
        .map(
          (s) => <String, dynamic>{
            'urls': s.urls,
            if (s.username != null && s.username!.isNotEmpty)
              'username': s.username,
            if (s.credential != null && s.credential!.isNotEmpty)
              'credential': s.credential,
          },
        )
        .toList();
  }

  String get serverUrl => _signaling.serverUrl;
  bool get isLoggedIn => keypair != null;
  bool get isReconnecting => status == AppStatus.reconnecting;

  /// The provisioning link currently being used. Populated by deep links,
  /// QR scans, and pasted links, and kept while the registration screen is up
  /// so a failed attempt can be retried without losing the code.
  OcnKsimUri? pendingProvision;

  void setProvisionIntent(OcnKsimUri provision) {
    pendingProvision = provision;
    if (keypair == null) notifyListeners();
  }

  void clearProvisionIntent() {
    pendingProvision = null;
    if (keypair == null) notifyListeners();
  }

  AppState({required String serverUrl})
    : _signaling = SignalingClient(serverUrl: serverUrl);

  Future<void> initialize() async {
    final exists = await KSimStorage.exists();
    log('kSIM exists: $exists');

    if (!exists) {
      status = AppStatus.needsRegistration;
      notifyListeners();
      return;
    }

    // Auto-login: load kSIM without passphrase
    status = AppStatus.connecting;
    notifyListeners();

    try {
      final result = await KSimStorage.load('');
      if (result != null) {
        final (kp, stored) = result;
        keypair = kp;
        displayName = stored.displayName;
        phoneNumber = OcnPhoneNumber(
          areaCode: stored.areaCode,
          number: stored.phoneNumber,
        );
        log('kSIM loaded: ${stored.phoneNumber} (${stored.displayName})');
        _connectToServer(stored.serverUrl);
      } else {
        log('kSIM load returned null');
        status = AppStatus.needsRegistration;
        notifyListeners();
      }
    } catch (e) {
      log('Auto-login failed: $e');
      status = AppStatus.needsRegistration;
      notifyListeners();
    }
  }

  Future<void> register(
    String serverUrl,
    String name, {
    String activationToken = '',
  }) async {
    displayName = name;
    status = AppStatus.connecting;
    notifyListeners();

    keypair = await KSimKeypair.generate();

    _signaling.serverUrl = serverUrl;
    _signaling.setCredentials(keypair!, name);
    _setupSignalingCallbacks();
    _signaling.connect();

    await Future.delayed(const Duration(milliseconds: 500));
    await _signaling.register(keypair!, name, activationToken: activationToken);
  }

  /// Called by the OS deep-link handler when an ocnksim:// URI arrives.
  void handleDeepLink(String raw) {
    final parsed = OcnKsimUri.parse(raw);
    if (parsed == null) {
      log('Deep link ignored (not ocnksim provisioning): $raw');
      return;
    }
    if (keypair != null) {
      log('Deep link ignored (already provisioned)');
      return;
    }
    log('Deep link provisioning for ${parsed.serverUrl}');
    setProvisionIntent(parsed);
  }

  void _connectToServer(String serverUrl) {
    _signaling.serverUrl = serverUrl;
    _signaling.setCredentials(keypair!, displayName);
    _setupSignalingCallbacks();
    _signaling.connect();

    Future.delayed(const Duration(milliseconds: 500), () async {
      await _signaling.register(keypair!, displayName);
    });
  }

  /// Set FCM token and register with server
  void setFCMToken(String token) {
    _fcmToken = token;
    if (_signaling.isConnected) {
      _signaling.registerFCM(token);
    }
  }

  /// Called when an FCM push says a call is waiting for us. Force a reconnect
  /// (ignoring backoff) so the server delivers the queued incoming call.
  void wakeForIncomingCall() {
    log('AppState: FCM incoming call — forcing reconnect');
    if (keypair == null) return;
    if (_signaling.connectionState != OcnConnectionState.connected) {
      _signaling.reconnectNow();
    }
  }

  Future<void> _startRinger() async {
    if (_ringer != null) return;
    log('AppState: starting ringer');
    try {
      final player = AudioPlayer();
      await player.setReleaseMode(ReleaseMode.loop);
      await player.setVolume(1.0);
      await player.play(AssetSource('ringtone/ring.wav'));
      _ringer = player;
    } catch (e) {
      log('AppState: ringer start failed: $e');
    }
  }

  Future<void> _stopRinger() async {
    final player = _ringer;
    _ringer = null;
    if (player == null) return;
    log('AppState: stopping ringer');
    try {
      await player.stop();
      await player.dispose();
    } catch (e) {
      log('AppState: ringer stop failed: $e');
    }
  }

  Future<void> logout() async {
    _fcmToken = null;
    _signaling.disconnect();
    _cleanupCall();
    await KSimStorage.deleteAll();
    keypair = null;
    phoneNumber = null;
    displayName = '';
    pendingProvision = null;
    status = AppStatus.needsRegistration;
    notifyListeners();
  }

  void _setupSignalingCallbacks() {
    _signaling.onConnectionState = (state) {
      if (state == OcnConnectionState.reconnecting) {
        status = AppStatus.reconnecting;
        notifyListeners();
      } else if (state == OcnConnectionState.connected &&
          status == AppStatus.reconnecting) {
        // Reconnected — status will be set to connected by onRegistered
      }
    };

    _signaling.onRegistered = (number) async {
      phoneNumber = number;
      status = AppStatus.connected;
      notifyListeners();

      // Re-register FCM token after reconnect
      if (_fcmToken != null) {
        _signaling.registerFCM(_fcmToken!);
      }

      // Save to database
      if (keypair != null) {
        await KSimStorage.save(
          keypair: keypair!,
          displayName: displayName,
          serverUrl: _signaling.serverUrl,
          phoneNumber: number.number,
          passphrase: '',
        );
        log('kSIM saved to database');
      }
    };

    _signaling.onIncomingCall = (callId, caller, callerName, sdp) {
      log(
        'AppState: incoming call $callId from ${caller.formatted} ($callerName)',
      );
      // Clear any FCM full-screen notification; the WebSocket delivery means
      // the call is real, and the ringer below takes over.
      CallNotifications.cancelIncomingCall(callId);
      activeCall = CallSession(
        callId: callId,
        remoteNumber: caller.formatted,
        remoteName: callerName,
        isIncoming: true,
        state: CallState.ringing,
      );
      activeCall!.pendingOfferSdp = sdp;
      notifyListeners();
      _startRinger();
    };

    _signaling.onCallRinging = (callId) {
      log('AppState: call ringing $callId');
      if (activeCall != null) {
        activeCall!.callId = callId;
        activeCall!.state = CallState.ringing;
        notifyListeners();
        _flushLocalIce();
        // Caller hears a ringback tone in the earpiece until the other side
        // picks up. (Service calls never reach ringing — they connect
        // immediately.)
        if (!activeCall!.isIncoming && !activeCall!.isService) {
          Ringback.start();
        }
      }
    };

    _signaling
        .onCallConnected = (callId, sdpAnswer, serviceCode, serviceName) async {
      log(
        'AppState: call connected $callId (hasAnswer=${sdpAnswer != null}, service=$serviceName)',
      );
      // Match by callId, or accept if we're an outgoing call with no callId yet (service calls skip ringing)
      if (activeCall != null &&
          (activeCall!.callId == callId ||
              (activeCall!.callId.isEmpty && !activeCall!.isIncoming))) {
        activeCall!.callId = callId;
        activeCall!.state = CallState.connected;
        activeCall!.serviceCode = serviceCode;
        activeCall!.serviceName = serviceName;
        notifyListeners();

        // Ringback ends the moment the call is picked up.
        await Ringback.stop();

        if (sdpAnswer != null && activeCall?.peerConnection != null) {
          try {
            await _webrtc.setRemoteDescription(
              activeCall!.peerConnection!,
              RTCSessionDescription(sdpAnswer, 'answer'),
            );
            activeCall!.remoteDescriptionSet = true;
            log('Remote description set from answer');
          } catch (e) {
            log('Failed to set remote description: $e');
          }
        }
        _flushLocalIce();
        _flushRemoteIce();
      }
    };

    _signaling.onCallEnded = (callId, reason) {
      log('AppState: call ended $callId reason=$reason');
      // Accept if callId matches, or if we have an active call (handles service calls)
      if (activeCall != null &&
          (activeCall!.callId == callId || callId.isEmpty)) {
        _cleanupCall();
        notifyListeners();
      }
    };
    _signaling.onICECandidate = (callId, candidate, sdpMid, sdpMLineIndex) {
      final ice = RTCIceCandidate(candidate, sdpMid, sdpMLineIndex);
      final call = activeCall;
      if (call == null) return;
      // Only accept candidates for the current call.
      if (call.callId != callId && call.callId.isNotEmpty) return;
      // A remote candidate can only be handed to the PeerConnection once the
      // peer connection exists AND the remote description is set. Anything
      // that arrives earlier (e.g. the caller's candidates while the callee is
      // still showing the incoming-call screen) is buffered and flushed after
      // the answer/remote-description is in place.
      if (call.peerConnection != null && call.remoteDescriptionSet) {
        _webrtc.addIceCandidate(call.peerConnection!, ice);
      } else {
        call.pendingRemoteIce.add(ice);
      }
    };

    _signaling.onError = (code, message) {
      log('AppState: error $code $message');
      if (status == AppStatus.connecting) {
        status = AppStatus.error;
        errorMessage = message;
      } else {
        errorMessage = message;
      }
      notifyListeners();
    };
  }

  Future<void> makeCall(String destination) async {
    if (keypair == null || status != AppStatus.connected) {
      log('Cannot call: keypair=${keypair != null} status=$status');
      return;
    }

    log('Making call to $destination');
    activeCall = CallSession(
      callId: '',
      remoteNumber: destination,
      remoteName: '',
      isIncoming: false,
      state: CallState.calling,
    );
    notifyListeners();

    try {
      final pc = await _webrtc.createPeerConnection(
        iceServers: _iceServersForCall(),
      );
      activeCall!.peerConnection = pc;

      final localStream = await _webrtc.getLocalStream();
      activeCall!.localStream = localStream;

      for (var track in localStream.getAudioTracks()) {
        pc.addTrack(track, localStream);
      }

      pc.onIceCandidate = (candidate) {
        final ice = RTCIceCandidate(
          candidate.candidate!,
          candidate.sdpMid ?? '',
          candidate.sdpMLineIndex ?? 0,
        );
        if (activeCall != null && activeCall!.callId.isNotEmpty) {
          _signaling.sendICECandidate(
            activeCall!.callId,
            ice.candidate!,
            ice.sdpMid!,
            ice.sdpMLineIndex!,
          );
        } else if (activeCall != null) {
          activeCall!.pendingLocalIce.add(ice);
        }
      };

      pc.onTrack = (event) {
        if (event.streams.isNotEmpty) {
          activeCall!.remoteStream = event.streams[0];
          notifyListeners();
        }
      };

      pc.onIceConnectionState = (state) {
        log('AppState: ICE connection state: $state (${activeCall?.callId})');
        debugPrint('ICE connection state: $state');
      };

      final offer = await _webrtc.createOffer(pc);
      _signaling.call(destination, offer.sdp!);
    } catch (e) {
      log('Call failed: $e');
      errorMessage = 'Call failed: $e';
      _cleanupCall();
      notifyListeners();
    }
  }

  Future<void> answerCall() async {
    if (activeCall == null || !activeCall!.isIncoming) return;

    log('Answering call ${activeCall!.callId}');
    await _stopRinger();

    try {
      final pc = await _webrtc.createPeerConnection(
        iceServers: _iceServersForCall(),
      );
      activeCall!.peerConnection = pc;

      final localStream = await _webrtc.getLocalStream();
      activeCall!.localStream = localStream;

      for (var track in localStream.getAudioTracks()) {
        pc.addTrack(track, localStream);
      }

      pc.onIceCandidate = (candidate) {
        final ice = RTCIceCandidate(
          candidate.candidate!,
          candidate.sdpMid ?? '',
          candidate.sdpMLineIndex ?? 0,
        );
        if (activeCall != null && activeCall!.callId.isNotEmpty) {
          _signaling.sendICECandidate(
            activeCall!.callId,
            ice.candidate!,
            ice.sdpMid!,
            ice.sdpMLineIndex!,
          );
        } else if (activeCall != null) {
          activeCall!.pendingLocalIce.add(ice);
        }
      };

      pc.onTrack = (event) {
        if (event.streams.isNotEmpty) {
          activeCall!.remoteStream = event.streams[0];
          notifyListeners();
        }
      };

      pc.onIceConnectionState = (state) {
        log('AppState: ICE connection state: $state (${activeCall?.callId})');
        debugPrint('ICE connection state: $state');
      };

      if (activeCall!.pendingOfferSdp != null) {
        await _webrtc.setRemoteDescription(
          pc,
          RTCSessionDescription(activeCall!.pendingOfferSdp!, 'offer'),
        );
        activeCall!.remoteDescriptionSet = true;
        // Now that the remote description exists we can add the caller's ICE
        // candidates that arrived while we were ringing.
        _flushRemoteIce();
      }

      final answer = await _webrtc.createAnswer(pc);
      _signaling.answerCall(activeCall!.callId, answer.sdp!);

      activeCall!.state = CallState.connected;
      notifyListeners();
    } catch (e) {
      log('Answer failed: $e');
      errorMessage = 'Answer failed: $e';
      _cleanupCall();
      notifyListeners();
    }
  }

  void hangup() {
    if (activeCall != null) {
      log('Hanging up call ${activeCall!.callId}');
      _signaling.hangup(activeCall!.callId);
      _cleanupCall();
      notifyListeners();
    }
  }

  void declineCall() {
    if (activeCall != null && activeCall!.isIncoming) {
      _signaling.hangup(activeCall!.callId);
      _cleanupCall();
      notifyListeners();
    }
  }

  void clearError() {
    errorMessage = null;
  }

  void toggleMute() {
    isMuted = !isMuted;
    activeCall?.localStream?.getAudioTracks().forEach((track) {
      track.enabled = !isMuted;
    });
    notifyListeners();
  }

  void toggleSpeaker() {
    isSpeaker = !isSpeaker;
    _webrtc.setSpeakerphone(isSpeaker);
    notifyListeners();
  }

  void _cleanupCall() {
    final call = activeCall;
    _stopRinger();
    Ringback.stop();
    if (call != null) {
      CallNotifications.cancelIncomingCall(call.callId);
    }
    call?.localStream?.dispose();
    call?.peerConnection?.close();
    activeCall = null;
    isMuted = false;
    isSpeaker = false;
  }

  void _flushLocalIce() {
    if (activeCall == null) return;
    for (final ice in activeCall!.pendingLocalIce) {
      _signaling.sendICECandidate(
        activeCall!.callId,
        ice.candidate!,
        ice.sdpMid!,
        ice.sdpMLineIndex!,
      );
    }
    activeCall!.pendingLocalIce.clear();
  }

  void _flushRemoteIce() {
    if (activeCall == null || activeCall!.peerConnection == null) return;
    if (!activeCall!.remoteDescriptionSet) return;
    for (final ice in activeCall!.pendingRemoteIce) {
      _webrtc.addIceCandidate(activeCall!.peerConnection!, ice);
    }
    activeCall!.pendingRemoteIce.clear();
  }

  @override
  void dispose() {
    _cleanupCall();
    _signaling.disconnect();
    super.dispose();
  }
}

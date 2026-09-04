import 'dart:async';
import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'dart:typed_data';
import 'package:audioplayers/audioplayers.dart';
import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import '../core/ksim/ksim.dart';
import '../core/notifications/call_notifications.dart';
import '../core/provision/ocn_ksim_uri.dart';
import '../core/audio/ringback.dart';
import '../core/signaling/signaling_client.dart';
import '../core/webrtc/webrtc_manager.dart';
import 'db/db.dart';
import 'phone/number_format.dart';

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

  /// Id of the in-flight [CallLogEntry] for this call, once recorded.
  String? logId;
  DateTime? connectedAt;
  bool logFinalized = false;

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

  final ContactStore _contacts = ContactStore();
  final CallHistoryStore _history = CallHistoryStore();
  final VoicemailStore _voicemail = VoicemailStore();
  final DmStore _dm = DmStore();
  bool _phoneDataLoaded = false;
  static int _idCounter = 0;

  final Map<String, Completer<Uint8List>> _audioWaiters = {};
  final Map<String, Completer<Uint8List>> _dmImageWaiters = {};

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

  // ---- Persistent phonebook (contacts + call history) ----

  String? get _ownArea => phoneNumber?.areaCode;
  List<Contact> get contacts => _contacts.items;
  List<CallLogEntry> get callHistory => _history.items;

  // ---- Voicemail ----

  List<VoicemailMessage> get voicemail {
    final items = [..._voicemail.items]
      ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    return List.unmodifiable(items);
  }

  int get voicemailUnread => _voicemail.unread;

  Future<void> refreshVoicemail() async {
    await _ensurePhoneData();
    if (_signaling.isConnected) {
      _signaling.voicemailList();
    }
  }

  Future<Uint8List> fetchVoicemailAudio(String id) async {
    final completer = Completer<Uint8List>();
    _audioWaiters[id] = completer;
    _signaling.voicemailGet(id);
    try {
      return await completer.future.timeout(const Duration(seconds: 20));
    } finally {
      _audioWaiters.remove(id);
    }
  }

  Future<void> deleteVoicemail(String id) async {
    await _ensurePhoneData();
    _voicemail.remove(id);
    await _voicemail.persist();
    _signaling.voicemailDelete(id);
    notifyListeners();
  }

  Future<void> markVoicemailRead(String id) async {
    await _ensurePhoneData();
    _voicemail.markRead(id);
    await _voicemail.persist();
    _signaling.voicemailMarkRead(id);
    notifyListeners();
  }

  void _onVoicemailList(List<VoicemailMessage> messages, int unread) {
    _voicemail.replaceAll(messages);
    _voicemail.persist();
    log('AppState: voicemail list updated (${messages.length}, $unread unread)');
    notifyListeners();
  }

  void _onVoicemailAudio(String id, Uint8List bytes) {
    final c = _audioWaiters.remove(id);
    if (c != null && !c.isCompleted) c.complete(bytes);
  }

  void _onVoicemailEvent(String callerNumber, String callerName, int unread) {
    log('AppState: voicemail event ($callerNumber) unread=$unread');
    // Refresh authoritative list from the server.
    _signaling.voicemailList();
    notifyListeners();
  }

  // ---- Direct messaging ----

  List<DmThread> get dmThreads => _dm.threads;
  int get dmUnread => _dm.unreadTotal;
  List<DmMessage> messagesFor(String peer) => _dm.messagesFor(peer);

  /// Mark a thread read (local only) and return its messages.
  List<DmMessage> openThread(String peer) {
    _dm.markPeerRead(peer);
    _dm.persist();
    notifyListeners();
    return _dm.messagesFor(peer);
  }

  Future<String> sendTextMessage(String peer, String text) async {
    await _ensurePhoneData();
    final clientId = _newId();
    final msg = DmMessage(
      id: clientId,
      clientId: clientId,
      peer: peer,
      direction: 'out',
      kind: 'text',
      text: text,
      status: 'sending',
      createdAt: DateTime.now(),
    );
    _dm.addOrUpdate(msg);
    await _dm.persist();
    notifyListeners();
    if (_signaling.isConnected) {
      _signaling.dmSend(
        to: peer,
        clientId: clientId,
        kind: 'text',
        text: text,
      );
    } else {
      _failMessage(clientId);
    }
    return clientId;
  }

  Future<String> sendImageMessage({
    required String peer,
    required Uint8List bytes,
    required String mime,
    String name = '',
  }) async {
    await _ensurePhoneData();
    final clientId = _newId();
    final path = await _dm.attachmentPathFor(
      clientId,
      ext: _extForMime(mime),
    );
    await File(path).writeAsBytes(bytes);
    final msg = DmMessage(
      id: clientId,
      clientId: clientId,
      peer: peer,
      direction: 'out',
      kind: 'image',
      imageName: name,
      imageMime: mime,
      imagePath: path,
      status: 'sending',
      createdAt: DateTime.now(),
    );
    _dm.addOrUpdate(msg);
    await _dm.persist();
    notifyListeners();
    if (_signaling.isConnected) {
      _signaling.dmSend(
        to: peer,
        clientId: clientId,
        kind: 'image',
        imageName: name,
        imageMime: mime,
        imageB64: base64Encode(bytes),
      );
    } else {
      _failMessage(clientId);
    }
    return clientId;
  }

  void _failMessage(String id) {
    final m = _dm.byId(id);
    if (m == null) return;
    final updated = m.copyWith(status: 'failed');
    _dm.removeById(id);
    _dm.addOrUpdate(updated);
    _dm.persist();
    notifyListeners();
  }

  /// Local path to an image message's bytes, downloading from the server on
  /// first access. Returns null if unavailable.
  Future<String?> imagePathFor(DmMessage message) async {
    final existing = message.imagePath;
    if (existing != null && await File(existing).exists()) return existing;
    if (!message.isImage) return null;
    if (message.direction == 'out') return null;

    final completer = Completer<Uint8List>();
    _dmImageWaiters[message.id] = completer;
    _signaling.dmAttachmentGet(message.id);
    try {
      final bytes = await completer.future.timeout(const Duration(seconds: 30));
      final path = await _dm.attachmentPathFor(
        message.id,
        ext: _extForMime(message.imageMime ?? 'image/jpeg'),
      );
      await File(path).writeAsBytes(bytes);
      _updateDmMessage(message, imagePath: path);
      _signaling.dmAck(message.id);
      return path;
    } catch (e) {
      log('AppState: image download failed for ${message.id}: $e');
      return null;
    } finally {
      _dmImageWaiters.remove(message.id);
    }
  }

  void _updateDmMessage(DmMessage original, {String? imagePath}) {
    final current = _dm.byId(original.id);
    if (current == null) return;
    _dm.removeById(original.id);
    _dm.addOrUpdate(current.copyWith(imagePath: imagePath));
    _dm.persist();
    notifyListeners();
  }

  void _onDmEvent(DmEventInfo event) {
    if (event.type == 'status') {
      final clientId = event.clientId;
      if (clientId != null) {
        final old = _dm.byClientId(clientId) ?? _dm.byId(clientId);
        if (old != null) {
          final replaced = DmMessage(
            id: event.messageId.isEmpty ? old.id : event.messageId,
            clientId: old.clientId,
            peer: old.peer,
            direction: old.direction,
            kind: old.kind,
            text: old.text,
            imageName: old.imageName,
            imageMime: old.imageMime,
            imagePath: old.imagePath,
            status: event.status ?? 'delivered',
            createdAt: old.createdAt,
          );
          _dm.removeById(old.id);
          _dm.addOrUpdate(replaced);
          _dm.persist();
          notifyListeners();
        }
      }
      return;
    }
    if (event.type != 'new' || event.from == null) return;
    final from = event.from!;
    final peer = from;
    final created = event.createdAt == null
        ? DateTime.now()
        : DateTime.fromMillisecondsSinceEpoch(event.createdAt!);
    final isImage = event.kind == 'image';
    final msg = DmMessage(
      id: event.messageId,
      peer: peer,
      direction: 'in',
      kind: event.kind ?? 'text',
      text: event.text ?? '',
      imageName: event.imageName,
      imageMime: event.imageMime,
      status: 'delivered',
      read: false,
      createdAt: created,
    );
    final isNew = _dm.addOrUpdate(msg);
    _dm.persist();
    notifyListeners();

    if (!isImage) {
      // Text is fully delivered — release the server outbox copy.
      _signaling.dmAck(event.messageId);
    } else {
      final stored = _dm.byId(event.messageId);
      if (stored == null || stored.imagePath == null) {
        // Prefetch image bytes (also acks once cached).
        imagePathFor(msg).then((_) {}).catchError((_) {});
      } else {
        _signaling.dmAck(event.messageId);
      }
    }
    if (isNew) {
      log('AppState: new dm from $from');
      CallNotifications.showMessage(
        fromNumber: event.from!,
        fromName: event.fromName ?? '',
      );
    }
  }

  void _onDmAttachment(String messageId, String name, String mime, String b64) {
    final c = _dmImageWaiters.remove(messageId);
    if (c != null && !c.isCompleted) {
      c.complete(base64Decode(b64));
    }
  }

  String _extForMime(String mime) {
    if (mime.contains('png')) return 'png';
    if (mime.contains('gif')) return 'gif';
    if (mime.contains('webp')) return 'webp';
    return 'jpg';
  }

  /// Contact matching [numberText] (dialed or caller number), or null.
  Contact? contactForNumber(String numberText) {
    final canon = canonicalNumber(numberText, ownArea: _ownArea);
    if (canon.isEmpty) return null;
    return _contacts.byNumber(canon);
  }

  Future<void> _ensurePhoneData() async {
    if (_phoneDataLoaded) return;
    _phoneDataLoaded = true;
    await _contacts.ensureLoaded();
    await _history.ensureLoaded();
    await _voicemail.ensureLoaded();
    await _dm.ensureLoaded();
    log('AppState: phonebook loaded '
        '(${contacts.length} contacts, ${callHistory.length} calls, ${voicemail.length} voicemails, ${_dm.items.length} messages)');
    notifyListeners();
  }

  String _newId() {
    _idCounter++;
    return '${DateTime.now().microsecondsSinceEpoch}-$_idCounter';
  }

  Future<void> addContact({required String name, required String number}) async {
    await _ensurePhoneData();
    final contact = Contact(
      id: _newId(),
      name: name.trim(),
      number: canonicalNumber(number, ownArea: _ownArea),
      createdAt: DateTime.now().millisecondsSinceEpoch,
    );
    if (contact.name.isEmpty || contact.number.isEmpty) return;
    await _contacts.add(contact);
    notifyListeners();
  }

  Future<void> updateContact(
    Contact contact, {
    required String name,
    required String number,
  }) async {
    await _ensurePhoneData();
    final updated = contact.copyWith(
      name: name.trim(),
      number: canonicalNumber(number, ownArea: _ownArea),
    );
    if (updated.name.isEmpty || updated.number.isEmpty) return;
    await _contacts.update(updated);
    notifyListeners();
  }

  Future<void> deleteContact(String id) async {
    await _ensurePhoneData();
    await _contacts.remove(id);
    notifyListeners();
  }

  Future<void> clearCallHistory() async {
    await _ensurePhoneData();
    await _history.clear();
    notifyListeners();
  }

  Future<void> deleteHistoryEntry(String id) async {
    await _ensurePhoneData();
    await _history.remove(id);
    notifyListeners();
  }

  void _startOutgoingLog(String destination) {
    final id = _newId();
    activeCall?.logId = id;
    _history.add(
      CallLogEntry(
        id: id,
        type: CallType.outgoing,
        number: canonicalNumber(destination, ownArea: _ownArea),
        startedAt: DateTime.now(),
      ),
    );
  }

  void _startIncomingLog(OcnPhoneNumber caller, String callerName) {
    activeCall?.logId = _newId();
    _history.add(
      CallLogEntry(
        id: activeCall!.logId!,
        type: CallType.incoming,
        number: canonicalNumber(caller.full, ownArea: _ownArea),
        nameSnapshot: callerName.isEmpty ? null : callerName,
        startedAt: DateTime.now(),
      ),
    );
  }

  void _markConnected() {
    final call = activeCall;
    if (call == null || call.connectedAt != null) return;
    call.connectedAt = DateTime.now();
    final id = call.logId;
    if (id != null) {
      _history.update(id, (e) => e.connectedAt = call.connectedAt);
    }
  }

  /// Resolves an in-flight call into its final history entry. Call only once
  /// (per call) from call teardown.
  void _finalizeCallLog(CallSession call, {bool declined = false}) {
    final id = call.logId;
    if (id == null || call.logFinalized) return;
    call.logFinalized = true;

    final now = DateTime.now();
    final answered = call.connectedAt != null;
    final CallType type;
    if (call.isIncoming) {
      if (declined) {
        type = CallType.rejected;
      } else if (!answered) {
        type = CallType.missed;
      } else {
        type = CallType.incoming;
      }
    } else {
      type = CallType.outgoing;
    }

    _history.update(id, (e) {
      e.type = type;
      e.endedAt = now;
      e.durationSec = answered
          ? now.difference(call.connectedAt!).inSeconds
          : 0;
      if (call.serviceCode != null) e.serviceCode = call.serviceCode;
      if (call.serviceName != null) e.serviceName = call.serviceName;
    });
  }

  // ---- End phonebook ----

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
        await _ensurePhoneData();
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

    await _ensurePhoneData();
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
          areaCode: number.areaCode,
        );
        log('kSIM saved to database');
      }

      // Fetch the mailbox (if any) now that we're registered.
      refreshVoicemail();
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
      _startIncomingLog(caller, callerName);
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
        if (serviceCode != null || serviceName != null) {
          final id = activeCall!.logId;
          if (id != null) {
            _history.update(id, (e) {
              if (serviceCode != null) e.serviceCode = serviceCode;
              if (serviceName != null) e.serviceName = serviceName;
            });
          }
        }
        _markConnected();
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
      // An older server replies "unknown message type" to messages it doesn't
      // know (e.g. voicemail). Treat that as "feature unsupported", not a
      // user-facing failure.
      if (message.contains('unknown message type') ||
          message.contains('invalid message type')) {
        log('AppState: ignoring unsupported-message error: $message');
        return;
      }
      log('AppState: error $code $message');
      if (status == AppStatus.connecting) {
        status = AppStatus.error;
        errorMessage = message;
      } else {
        errorMessage = message;
      }
      notifyListeners();
    };

    _signaling.onVoicemailList = _onVoicemailList;
    _signaling.onVoicemailAudio = _onVoicemailAudio;
    _signaling.onVoicemailEvent = _onVoicemailEvent;
    _signaling.onDmEvent = _onDmEvent;
    _signaling.onDmAttachment = _onDmAttachment;
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
    _startOutgoingLog(destination);
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
      _markConnected();
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
      log('Declining call ${activeCall!.callId}');
      // Tell the server to route the caller into our voicemail.
      _signaling.decline(activeCall!.callId);
      _cleanupCall(declined: true);
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

  void _cleanupCall({bool declined = false}) {
    final call = activeCall;
    _stopRinger();
    Ringback.stop();
    if (call != null) {
      CallNotifications.cancelIncomingCall(call.callId);
    }
    call?.localStream?.dispose();
    call?.peerConnection?.close();
    if (call != null) {
      _finalizeCallLog(call, declined: declined);
    }
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

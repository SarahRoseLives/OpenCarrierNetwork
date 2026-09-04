import 'dart:async';
import 'dart:convert';
import 'dart:developer' as dev;
import 'dart:math';
import 'dart:typed_data';
import 'package:web_socket_channel/web_socket_channel.dart';
import '../db/db.dart';
import '../ksim/ksim_keypair.dart';

class OcnPhoneNumber {
  final String areaCode;
  final String number;

  OcnPhoneNumber({required this.areaCode, required this.number});

  String get full => areaCode.isEmpty ? number : '$areaCode$number';
  String get formatted {
    final local = number.length >= 7
        ? '${number.substring(0, 3)}-${number.substring(3)}'
        : number;
    if (areaCode.isEmpty) return local;
    return '$areaCode-$local';
  }

  String get localFormatted {
    if (number.length >= 7) {
      return '${number.substring(0, 3)}-${number.substring(3)}';
    }
    return number;
  }

  factory OcnPhoneNumber.fromJson(Map<String, dynamic> json) => OcnPhoneNumber(
    areaCode: json['area_code'] as String,
    number: json['number'] as String,
  );
}

enum OcnConnectionState { disconnected, connecting, connected, reconnecting }

/// A WebRTC ICE server (STUN/TURN) as provided by the OCN server on
/// registration.
class IceServerConfig {
  final List<String> urls;
  final String? username;
  final String? credential;

  IceServerConfig({required this.urls, this.username, this.credential});
}

class SignalingClient {
  WebSocketChannel? _channel;
  String serverUrl;

  // Callbacks
  Function(OcnPhoneNumber number)? onRegistered;
  Function(String callId, OcnPhoneNumber caller, String callerName, String sdp)?
  onIncomingCall;
  Function(String callId)? onCallRinging;
  Function(
    String callId,
    String? sdpAnswer,
    String? serviceCode,
    String? serviceName,
  )?
  onCallConnected;
  Function(String callId, String reason)? onCallEnded;
  Function(String callId, String candidate, String sdpMid, int sdpMLineIndex)?
  onICECandidate;
  Function(int code, String message)? onError;
  Function(OcnConnectionState state)? onConnectionState;

  // Voicemail
  Function(List<VoicemailMessage> messages, int unread)? onVoicemailList;
  Function(String id, Uint8List oggBytes)? onVoicemailAudio;
  Function(String callerNumber, String callerName, int unread)? onVoicemailEvent;

  // Direct messaging
  Function(DmEventInfo event)? onDmEvent;
  Function(String messageId, String name, String mime, String b64)? onDmAttachment;

  // Internal state for reconnection
  OcnConnectionState _state = OcnConnectionState.disconnected;
  KSimKeypair? _keypair;
  String _displayName = '';
  Timer? _reconnectTimer;
  int _reconnectAttempt = 0;
  static const _maxBackoff = 30;
  bool _intentionalDisconnect = false;

  /// True when the next established socket must re-register even though we are
  /// in the `connecting` state (e.g. an FCM wake forcing an immediate connect).
  bool _reregisterOnReady = false;

  // Pending registration state
  KSimKeypair? _pendingKeypair;
  String? _pendingDisplayName;
  String _pendingActivationToken = '';
  Completer<void>? _challengeCompleter;

  /// STUN/TURN servers handed to us by the server on registration.
  List<IceServerConfig> _iceServers = [];

  List<IceServerConfig> get iceServers => List.unmodifiable(_iceServers);

  SignalingClient({required this.serverUrl});

  OcnConnectionState get connectionState => _state;
  bool get isConnected => _state == OcnConnectionState.connected;

  void connect() {
    if (_state == OcnConnectionState.connecting ||
        _state == OcnConnectionState.connected) {
      return;
    }
    _intentionalDisconnect = false;
    _reregisterOnReady = false;
    _setState(OcnConnectionState.connecting);
    _doConnect();
  }

  /// Drop any backoff delay and reconnect immediately. Used when an FCM push
  /// tells us a call is waiting — we can't afford to wait out a backoff timer.
  void reconnectNow() {
    if (_state == OcnConnectionState.connected ||
        _state == OcnConnectionState.connecting) {
      return;
    }
    dev.log('Reconnecting immediately (FCM wake)');
    _intentionalDisconnect = false;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _reregisterOnReady = true;
    _setState(OcnConnectionState.connecting);
    _doConnect();
  }

  void _doConnect() {
    final chan = WebSocketChannel.connect(Uri.parse(serverUrl));
    _channel = chan;
    var dead = false;

    // If a connect attempt neither succeeds nor fails (e.g. a silent network
    // black hole), abort it after a timeout so the reconnect loop keeps
    // cycling instead of stalling forever on a hung socket.
    final guard = Timer(const Duration(seconds: 10), () {
      if (dead) return;
      dead = true;
      dev.log('WebSocket connect timed out, will retry');
      try {
        chan.sink.close();
      } catch (_) {}
      _onDisconnected();
    });

    // A socket event only matters if it belongs to the current attempt.
    // Events from a superseded socket (e.g. one we tore down after a failed
    // re-register) must not disturb the live connection.
    bool current() => !dead && identical(chan, _channel);

    // Listen for connection to be ready, then re-register if reconnecting
    chan.ready
        .then((_) {
          if (!current()) return;
          guard.cancel();
          dev.log('WebSocket connected');
          if (_keypair != null &&
              (_state == OcnConnectionState.reconnecting ||
                  _reregisterOnReady)) {
            _reregisterOnReady = false;
            _reRegister();
          }
        })
        .catchError((e) {
          if (!current()) return;
          dead = true;
          guard.cancel();
          dev.log('WebSocket ready error: $e');
          _onDisconnected();
        });

    chan.stream.listen(
      (data) {
        if (!current()) return;
        final json = jsonDecode(data as String) as Map<String, dynamic>;
        _handleMessage(json);
      },
      onError: (error) {
        if (!current()) return;
        dead = true;
        guard.cancel();
        dev.log('WebSocket error: $error');
        _onDisconnected();
      },
      onDone: () {
        if (!current()) return;
        dead = true;
        guard.cancel();
        dev.log('WebSocket closed');
        _onDisconnected();
      },
    );
  }

  void _onDisconnected() {
    if (_intentionalDisconnect) {
      _setState(OcnConnectionState.disconnected);
      return;
    }

    if (_state == OcnConnectionState.connected ||
        _state == OcnConnectionState.connecting) {
      _setState(OcnConnectionState.reconnecting);
      _scheduleReconnect();
    } else if (_state == OcnConnectionState.reconnecting) {
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    _reconnectTimer?.cancel();
    final delay = _backoffDelay();
    dev.log('Reconnecting in ${delay}s (attempt $_reconnectAttempt)');
    _reconnectTimer = Timer(Duration(seconds: delay), () {
      _doConnect();
    });
  }

  int _backoffDelay() {
    final delay = min(pow(2, _reconnectAttempt).toInt(), _maxBackoff);
    _reconnectAttempt++;
    return delay;
  }

  void _resetBackoff() {
    _reconnectAttempt = 0;
  }

  void _setState(OcnConnectionState newState) {
    if (_state != newState) {
      _state = newState;
      dev.log('Connection state: $newState');
      onConnectionState?.call(newState);
    }
  }

  /// Store credentials for automatic re-registration after reconnect
  void setCredentials(KSimKeypair keypair, String displayName) {
    _keypair = keypair;
    _displayName = displayName;
  }

  void disconnect() {
    _intentionalDisconnect = true;
    _reregisterOnReady = false;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _channel?.sink.close();
    _channel = null;
    _setState(OcnConnectionState.disconnected);
    _challengeCompleter?.completeError('Disconnected');
  }

  void _handleMessage(Map<String, dynamic> json) {
    dev.log('Received: ${json.keys.join(", ")}');

    if (json.containsKey('challenge_response')) {
      _handleChallengeResponse(
        json['challenge_response'] as Map<String, dynamic>,
      );
    } else if (json.containsKey('register_response')) {
      _handleRegisterResponse(
        json['register_response'] as Map<String, dynamic>,
      );
    } else if (json.containsKey('incoming_call')) {
      final call = json['incoming_call'] as Map<String, dynamic>;
      dev.log(
        'Incoming call: ${call['call_id']} from ${call['caller_number']}',
      );
      final callerNum = OcnPhoneNumber.fromJson(
        call['caller_number'] as Map<String, dynamic>,
      );
      final callerName =
          (call['caller_name'] as Map<String, dynamic>?)?['name'] as String? ??
          '';
      final offer = call['offer'] as Map<String, dynamic>;
      onIncomingCall?.call(
        call['call_id'] as String,
        callerNum,
        callerName,
        offer['sdp'] as String,
      );
    } else if (json.containsKey('call_ringing')) {
      onCallRinging?.call(json['call_ringing']['call_id'] as String);
    } else if (json.containsKey('call_connected')) {
      final connected = json['call_connected'] as Map<String, dynamic>;
      final answer = connected['answer'] as Map<String, dynamic>?;
      final service = connected['service'] as Map<String, dynamic>?;
      onCallConnected?.call(
        connected['call_id'] as String,
        answer?['sdp'] as String?,
        service?['code'] as String?,
        service?['name'] as String?,
      );
    } else if (json.containsKey('call_ended')) {
      final ended = json['call_ended'] as Map<String, dynamic>;
      onCallEnded?.call(ended['call_id'] as String, ended['reason'] as String);
    } else if (json.containsKey('ice_candidate')) {
      final ice = json['ice_candidate'] as Map<String, dynamic>;
      final candidate = ice['candidate'] as Map<String, dynamic>;
      onICECandidate?.call(
        ice['call_id'] as String,
        candidate['candidate'] as String,
        candidate['sdp_mid'] as String,
        candidate['sdp_mline_index'] as int,
      );
    } else if (json.containsKey('error')) {
      final err = json['error'] as Map<String, dynamic>;
      dev.log('Error from server: ${err['code']} ${err['message']}');
      onError?.call(err['code'] as int, err['message'] as String);
      _challengeCompleter?.completeError(err['message'] as String);
    } else if (json.containsKey('voicemail_list_response')) {
      final v = json['voicemail_list_response'] as Map<String, dynamic>;
      final messages = (v['messages'] as List<dynamic>? ?? [])
          .map((e) => VoicemailMessage.fromJson((e as Map).cast<String, dynamic>()))
          .toList();
      onVoicemailList?.call(messages, v['unread'] as int? ?? 0);
    } else if (json.containsKey('voicemail_get_response')) {
      final v = json['voicemail_get_response'] as Map<String, dynamic>;
      final b64 = v['audio_b64'] as String? ?? '';
      onVoicemailAudio?.call(v['id'] as String, base64Decode(b64));
    } else if (json.containsKey('voicemail_event')) {
      final v = json['voicemail_event'] as Map<String, dynamic>;
      onVoicemailEvent?.call(
        v['caller_number'] as String? ?? '',
        v['caller_name'] as String? ?? '',
        v['unread'] as int? ?? 0,
      );
    } else if (json.containsKey('dm_event')) {
      onDmEvent?.call(
        DmEventInfo.fromJson(
          (json['dm_event'] as Map).cast<String, dynamic>(),
        ),
      );
    } else if (json.containsKey('dm_attachment')) {
      final v = json['dm_attachment'] as Map<String, dynamic>;
      onDmAttachment?.call(
        v['message_id'] as String? ?? '',
        v['name'] as String? ?? '',
        v['mime'] as String? ?? '',
        v['b64'] as String? ?? '',
      );
    }
  }

  void _handleChallengeResponse(Map<String, dynamic> json) {
    if (_pendingKeypair == null || _challengeCompleter == null) {
      return;
    }

    final nonce = base64Decode(json['nonce'] as String);
    final timestamp = json['timestamp'] as int;

    _pendingKeypair!
        .signChallenge(Uint8List.fromList(nonce), timestamp)
        .then((signature) {
          _send({
            'register': {
              'ksim_id': {'public_key': _pendingKeypair!.encodePublicKey()},
              'challenge_response': {'signature': base64Encode(signature)},
              'display_name': {'name': _pendingDisplayName ?? ''},
              if (_pendingActivationToken.isNotEmpty)
                'activation_token': _pendingActivationToken,
            },
          });
          _challengeCompleter?.complete();
        })
        .catchError((e) {
          _challengeCompleter?.completeError(e);
        });
  }

  void _handleRegisterResponse(Map<String, dynamic> json) {
    if (json['success'] == true && json['assigned_number'] != null) {
      final num = OcnPhoneNumber.fromJson(
        json['assigned_number'] as Map<String, dynamic>,
      );

      // Capture any STUN/TURN servers the server provides.
      final rawIce = json['ice_servers'];
      if (rawIce is List) {
        _iceServers = rawIce.map<IceServerConfig>((e) {
          final m = (e as Map).cast<String, dynamic>();
          return IceServerConfig(
            urls: (m['urls'] as List).cast<String>(),
            username: m['username'] as String?,
            credential: m['credential'] as String?,
          );
        }).toList();
      }

      _setState(OcnConnectionState.connected);
      _resetBackoff();
      onRegistered?.call(num);
    } else {
      onError?.call(
        400,
        json['error_message'] as String? ?? 'Registration failed',
      );
    }
  }

  /// Request a challenge from the server, then register. When [activationToken]
  /// is provided and this is a brand-new identity, the server provisions a
  /// number from the admin-issued code.
  Future<void> register(
    KSimKeypair keypair,
    String displayName, {
    String activationToken = '',
  }) async {
    _pendingKeypair = keypair;
    _pendingDisplayName = displayName;
    _pendingActivationToken = activationToken;
    _challengeCompleter = Completer<void>();

    _send({
      'challenge_request': {
        'ksim_id': {'public_key': keypair.encodePublicKey()},
      },
    });

    await _challengeCompleter!.future;
  }

  /// Re-register after reconnection using stored credentials.
  ///
  /// Reconnection runs while `_state` is still `reconnecting`, so this can be
  /// called on a live socket even though we aren't `connected` yet. If the
  /// registration fails for a transient reason, drop the socket and kick the
  /// reconnect loop rather than leaving the client wedged on the reconnecting
  /// banner.
  Future<void> _reRegister() async {
    if (_keypair == null) return;
    dev.log('Re-registering after reconnect...');
    try {
      await register(_keypair!, _displayName);
      dev.log('Re-registration successful');
    } catch (e) {
      dev.log('Re-registration failed: $e');
      final ch = _channel;
      _channel = null;
      try {
        ch?.sink.close();
      } catch (_) {}
      _scheduleReconnect();
    }
  }

  void call(String destination, String sdp) {
    dev.log('Calling $destination');
    _send({
      'call': {
        'destination': destination,
        'offer': {'sdp': sdp, 'type': 'offer'},
      },
    });
  }

  void answerCall(String callId, String sdp) {
    _send({
      'call_answer': {
        'call_id': callId,
        'answer': {'sdp': sdp, 'type': 'answer'},
      },
    });
  }

  void hangup(String callId) {
    _send({
      'call_hangup': {'call_id': callId},
    });
  }

  /// Explicitly decline an incoming call so the server routes the caller to
  /// voicemail rather than just ending the call.
  void decline(String callId) {
    _send({
      'call_decline': {'call_id': callId},
    });
  }

  void sendICECandidate(
    String callId,
    String candidate,
    String sdpMid,
    int sdpMLineIndex,
  ) {
    _send({
      'ice_candidate': {
        'call_id': callId,
        'candidate': {
          'candidate': candidate,
          'sdp_mid': sdpMid,
          'sdp_mline_index': sdpMLineIndex,
        },
      },
    });
  }

  void registerFCM(String token) {
    _send({
      'register_fcm': {'token': token},
    });
  }

  // ---- Voicemail ----

  void voicemailList() {
    _send({'voicemail_list': <String, dynamic>{}});
  }

  void voicemailGet(String id) {
    _send({'voicemail_get': {'id': id}});
  }

  void voicemailDelete(String id) {
    _send({'voicemail_delete': {'id': id}});
  }

  void voicemailMarkRead(String id) {
    _send({'voicemail_mark_read': {'id': id}});
  }

  // ---- Direct messaging ----

  void dmSend({
    required String to,
    required String clientId,
    required String kind,
    String text = '',
    String imageName = '',
    String imageMime = '',
    String imageB64 = '',
  }) {
    _send({
      'dm_send': {
        'to': to,
        'client_id': clientId,
        'kind': kind,
        if (text.isNotEmpty) 'text': text,
        if (kind == 'image' && imageB64.isNotEmpty)
          'image': {
            'name': imageName,
            'mime': imageMime,
            'b64': imageB64,
          },
      },
    });
  }

  void dmAck(String messageId) {
    _send({'dm_ack': {'message_id': messageId}});
  }

  void dmAttachmentGet(String messageId) {
    _send({'dm_attachment_get': {'message_id': messageId}});
  }

  void ping() {
    _send({'ping': {}});
  }

  void _send(Map<String, dynamic> message) {
    // Sending is allowed on any live (or being-established) socket. This must
    // include `reconnecting`: after a drop we open a fresh socket and send the
    // challenge/register to recover, and that happens before the state flips
    // back to `connected`.
    if (_state == OcnConnectionState.disconnected || _channel == null) {
      dev.log('Cannot send: not connected');
      return;
    }
    try {
      _channel?.sink.add(jsonEncode(message));
    } catch (e) {
      dev.log('Send error: $e');
    }
  }
}

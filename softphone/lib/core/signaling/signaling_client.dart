import 'dart:async';
import 'dart:convert';
import 'dart:developer';
import 'dart:typed_data';
import 'package:web_socket_channel/web_socket_channel.dart';
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

class SignalingClient {
  WebSocketChannel? _channel;
  String serverUrl;

  // Callbacks
  Function(OcnPhoneNumber number)? onRegistered;
  Function(String callId, OcnPhoneNumber caller, String callerName, String sdp)? onIncomingCall;
  Function(String callId)? onCallRinging;
  Function(String callId, String? sdpAnswer, String? serviceCode, String? serviceName)? onCallConnected;
  Function(String callId, String reason)? onCallEnded;
  Function(String callId, String candidate, String sdpMid, int sdpMLineIndex)? onICECandidate;
  Function(int code, String message)? onError;

  // Pending registration state
  KSimKeypair? _pendingKeypair;
  String? _pendingDisplayName;
  Completer<void>? _challengeCompleter;

  SignalingClient({required this.serverUrl});

  void connect() {
    _channel = WebSocketChannel.connect(Uri.parse(serverUrl));
    _channel!.stream.listen(
      (data) {
        final json = jsonDecode(data as String) as Map<String, dynamic>;
        _handleMessage(json);
      },
      onError: (error) {
        onError?.call(0, 'WebSocket error: $error');
        _challengeCompleter?.completeError(error);
      },
      onDone: () {
        onError?.call(0, 'WebSocket closed');
        _challengeCompleter?.completeError('WebSocket closed');
      },
    );
  }

  void disconnect() {
    _channel?.sink.close();
    _channel = null;
    _challengeCompleter?.completeError('Disconnected');
  }

  void _handleMessage(Map<String, dynamic> json) {
    log('Received: ${json.keys.join(", ")}');

    if (json.containsKey('challenge_response')) {
      _handleChallengeResponse(json['challenge_response'] as Map<String, dynamic>);
    } else if (json.containsKey('register_response')) {
      _handleRegisterResponse(json['register_response'] as Map<String, dynamic>);
    } else if (json.containsKey('incoming_call')) {
      final call = json['incoming_call'] as Map<String, dynamic>;
      log('Incoming call: ${call['call_id']} from ${call['caller_number']}');
      final callerNum = OcnPhoneNumber.fromJson(call['caller_number'] as Map<String, dynamic>);
      final callerName = (call['caller_name'] as Map<String, dynamic>?)?['name'] as String? ?? '';
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
      log('Error from server: ${err['code']} ${err['message']}');
      onError?.call(err['code'] as int, err['message'] as String);
      _challengeCompleter?.completeError(err['message'] as String);
    }
  }

  void _handleChallengeResponse(Map<String, dynamic> json) {
    if (_pendingKeypair == null || _challengeCompleter == null) {
      return;
    }

    final nonce = base64Decode(json['nonce'] as String);
    final timestamp = json['timestamp'] as int;

    // Sign the challenge and complete
    _pendingKeypair!.signChallenge(Uint8List.fromList(nonce), timestamp).then((signature) {
      _send({
        'register': {
          'ksim_id': {
            'public_key': _pendingKeypair!.encodePublicKey(),
          },
          'challenge_response': {
            'signature': base64Encode(signature),
          },
          'display_name': {
            'name': _pendingDisplayName ?? '',
          },
        },
      });
      _challengeCompleter?.complete();
    }).catchError((e) {
      _challengeCompleter?.completeError(e);
    });
  }

  void _handleRegisterResponse(Map<String, dynamic> json) {
    if (json['success'] == true && json['assigned_number'] != null) {
      final num = OcnPhoneNumber.fromJson(json['assigned_number'] as Map<String, dynamic>);
      onRegistered?.call(num);
    } else {
      onError?.call(400, json['error_message'] as String? ?? 'Registration failed');
    }
  }

  /// Request a challenge from the server, then register
  Future<void> register(KSimKeypair keypair, String displayName) async {
    _pendingKeypair = keypair;
    _pendingDisplayName = displayName;
    _challengeCompleter = Completer<void>();

    // Step 1: Request challenge
    _send({
      'challenge_request': {
        'ksim_id': {
          'public_key': keypair.encodePublicKey(),
        },
      },
    });

    // Step 2: Wait for challenge response to be handled
    // The _handleChallengeResponse method will sign and send the registration
    await _challengeCompleter!.future;
  }

  void call(String destination, String sdp) {
    log('Calling $destination');
    _send({
      'call': {
        'destination': destination,
        'offer': {
          'sdp': sdp,
          'type': 'offer',
        },
      },
    });
  }

  void answerCall(String callId, String sdp) {
    _send({
      'call_answer': {
        'call_id': callId,
        'answer': {
          'sdp': sdp,
          'type': 'answer',
        },
      },
    });
  }

  void hangup(String callId) {
    _send({
      'call_hangup': {
        'call_id': callId,
      },
    });
  }

  void sendICECandidate(String callId, String candidate, String sdpMid, int sdpMLineIndex) {
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

  void ping() {
    _send({'ping': {}});
  }

  void _send(Map<String, dynamic> message) {
    _channel?.sink.add(jsonEncode(message));
  }
}

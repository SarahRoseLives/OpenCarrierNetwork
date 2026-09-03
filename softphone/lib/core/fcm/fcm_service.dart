import 'dart:async';
import 'dart:developer';
import 'dart:io';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import '../notifications/call_notifications.dart';

/// Callback when FCM delivers an incoming call notification
typedef OnFCMCall = void Function(String callId, String callerNumber, String callerName);

/// Callback when an FCM token becomes available (initial or refreshed).
typedef OnFCMToken = void Function(String token);

class FCMService {
  static FirebaseMessaging? _messaging;
  static String? _token;
  static OnFCMCall? _onCall;
  static OnFCMToken? _onToken;
  static StreamSubscription? _tokenSub;
  static Timer? _retryTimer;
  static int _retryAttempts = 0;

  /// Initialize Firebase and FCM. Android only.
  ///
  /// [onToken] is invoked whenever a token becomes available — at startup or
  /// later (retries / refresh) — so the caller can push it to the server even
  /// if Firebase was slow to hand one out.
  static Future<String?> init({OnFCMCall? onCall, OnFCMToken? onToken}) async {
    if (!Platform.isAndroid) {
      log('FCM: skipped (not Android)');
      return null;
    }

    _onCall = onCall;
    _onToken = onToken;

    try {
      await Firebase.initializeApp();
      _messaging = FirebaseMessaging.instance;

      // Set up the incoming-call notification channel (main isolate).
      // Tapping a notification just wakes the app — the call itself is
      // delivered over WebSocket after reconnect.
      await CallNotifications.initialize(onCallTap: (data) {
        _onCall?.call(
          data['call_id'] ?? '',
          data['caller_number'] ?? '',
          data['caller_name'] ?? '',
        );
      });

      _token = await _fetchTokenWithRetry();

      // Listen for token refresh
      _tokenSub = _messaging!.onTokenRefresh.listen((newToken) {
        log('FCM: token refreshed');
        _token = newToken;
        _onToken?.call(newToken);
      });

      // Handle foreground messages
      FirebaseMessaging.onMessage.listen(_handleForegroundMessage);

      // Handle background messages
      FirebaseMessaging.onBackgroundMessage(_handleBackgroundMessage);

      // Handle message when app opened from terminated state
      final initialMessage = await _messaging!.getInitialMessage();
      if (initialMessage != null) {
        log('FCM: opened from terminated state');
        // User tapped the notification to open the app — don't re-ring, just
        // wake the app so it reconnects and receives the queued call.
        _wakeOnly(initialMessage.data);
      }

      // Handle message when app opened from background
      FirebaseMessaging.onMessageOpenedApp.listen((message) {
        log('FCM: opened from background');
        _wakeOnly(message.data);
      });

      if (_token == null) {
        // Firebase did not hand out a token yet (e.g. freshly installed or
        // Play services still registering). Keep retrying in the background so
        // the token eventually reaches the server.
        _scheduleTokenRetry();
      }

      return _token;
    } catch (e) {
      log('FCM: init failed: $e');
      return null;
    }
  }

  /// Tries getToken a few times up front (short backoff), then gives up so the
  /// caller can fall back to the background retry timer.
  static Future<String?> _fetchTokenWithRetry() async {
    for (var i = 0; i < 3; i++) {
      try {
        final t = await _messaging!.getToken();
        if (t != null && t.isNotEmpty) {
          log('FCM: token acquired (len=${t.length})');
          _onToken?.call(t);
          return t;
        }
      } catch (e) {
        log('FCM: getToken attempt $i failed: $e');
      }
      await Future.delayed(const Duration(seconds: 2));
    }
    return null;
  }

  static void _scheduleTokenRetry() {
    _retryTimer?.cancel();
    _retryAttempts = 0;
    _retryTimer = Timer.periodic(const Duration(seconds: 20), (timer) async {
      _retryAttempts++;
      if (_retryAttempts > 30) {
        timer.cancel();
        return;
      }
      try {
        final t = await _messaging?.getToken();
        if (t != null && t.isNotEmpty) {
          log('FCM: token acquired on background retry');
          _token = t;
          _onToken?.call(t);
          timer.cancel();
        }
      } catch (e) {
        log('FCM: background token retry failed: $e');
      }
    });
  }

  static String? get token => _token;

  static void _handleForegroundMessage(RemoteMessage message) {
    log('FCM: foreground message: ${message.data}');
    _handleMessageData(message.data);
  }

  static void _wakeOnly(Map<String, dynamic> data) {
    final callId = data['call_id'] as String? ?? '';
    final callerNumber = data['caller_number'] as String? ?? '';
    final callerName = data['caller_name'] as String? ?? '';
    _onCall?.call(callId, callerNumber, callerName);
  }

  static void _handleMessageData(Map<String, dynamic> data) {
    final type = data['type'] as String?;
    if (type == 'incoming_call') {
      final callId = data['call_id'] as String? ?? '';
      final callerNumber = data['caller_number'] as String? ?? '';
      final callerName = data['caller_name'] as String? ?? '';
      log('FCM: incoming call $callId from $callerNumber ($callerName)');

      // Ring via notification so the call is audible in every state.
      CallNotifications.showIncomingCall(
        callId: callId,
        callerNumber: callerNumber,
        callerName: callerName,
      );

      // Wake the app so it reconnects and receives the queued call.
      _onCall?.call(callId, callerNumber, callerName);
    }
  }

  static void dispose() {
    _tokenSub?.cancel();
    _retryTimer?.cancel();
    _retryTimer = null;
  }
}

/// Must be a top-level function for background message handling.
///
/// Runs in its own isolate when the app is in the background or has been
/// killed. We cannot touch the UI here, but we can show a full-screen
/// "incoming call" notification which rings and, when tapped / launched by the
/// OS, brings the app to the foreground to receive the call over WebSocket.
@pragma('vm:entry-point')
Future<void> _handleBackgroundMessage(RemoteMessage message) async {
  log('FCM: background message: ${message.data}');
  if (message.data['type'] != 'incoming_call') return;

  try {
    await Firebase.initializeApp();
  } catch (e) {
    log('FCM: background Firebase init failed: $e');
  }

  final callId = message.data['call_id'] as String? ?? '';
  final callerNumber = message.data['caller_number'] as String? ?? '';
  final callerName = message.data['caller_name'] as String? ?? '';
  if (callId.isEmpty) return;

  await CallNotifications.showIncomingCall(
    callId: callId,
    callerNumber: callerNumber,
    callerName: callerName,
  );
}

import 'dart:async';
import 'dart:developer';
import 'dart:io';
import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';

/// Callback when FCM delivers an incoming call notification
typedef OnFCMCall = void Function(String callId, String callerNumber, String callerName);

class FCMService {
  static FirebaseMessaging? _messaging;
  static String? _token;
  static OnFCMCall? _onCall;
  static StreamSubscription? _tokenSub;

  /// Initialize Firebase and FCM. Android only.
  static Future<String?> init({OnFCMCall? onCall}) async {
    if (!Platform.isAndroid) {
      log('FCM: skipped (not Android)');
      return null;
    }

    _onCall = onCall;

    try {
      await Firebase.initializeApp();
      _messaging = FirebaseMessaging.instance;

      // Request permission (Android 13+)
      final settings = await _messaging!.requestPermission(
        alert: true,
        badge: true,
        sound: true,
      );
      log('FCM: permission status: ${settings.authorizationStatus}');

      // Get token
      _token = await _messaging!.getToken();
      log('FCM: token=$_token');

      // Listen for token refresh
      _tokenSub = _messaging!.onTokenRefresh.listen((newToken) {
        log('FCM: token refreshed');
        _token = newToken;
      });

      // Handle foreground messages
      FirebaseMessaging.onMessage.listen(_handleForegroundMessage);

      // Handle background messages
      FirebaseMessaging.onBackgroundMessage(_handleBackgroundMessage);

      // Handle message when app opened from terminated state
      final initialMessage = await _messaging!.getInitialMessage();
      if (initialMessage != null) {
        log('FCM: opened from terminated state');
        _handleMessageData(initialMessage.data);
      }

      // Handle message when app opened from background
      FirebaseMessaging.onMessageOpenedApp.listen((message) {
        log('FCM: opened from background');
        _handleMessageData(message.data);
      });

      return _token;
    } catch (e) {
      log('FCM: init failed: $e');
      return null;
    }
  }

  static String? get token => _token;

  static void _handleForegroundMessage(RemoteMessage message) {
    log('FCM: foreground message: ${message.data}');
    _handleMessageData(message.data);
  }

  static void _handleMessageData(Map<String, dynamic> data) {
    final type = data['type'] as String?;
    if (type == 'incoming_call') {
      final callId = data['call_id'] as String? ?? '';
      final callerNumber = data['caller_number'] as String? ?? '';
      final callerName = data['caller_name'] as String? ?? '';
      log('FCM: incoming call $callId from $callerNumber ($callerName)');
      _onCall?.call(callId, callerNumber, callerName);
    }
  }

  static void dispose() {
    _tokenSub?.cancel();
  }
}

/// Must be a top-level function for background message handling
@pragma('vm:entry-point')
Future<void> _handleBackgroundMessage(RemoteMessage message) async {
  await Firebase.initializeApp();
  log('FCM: background message: ${message.data}');
  // Background messages can't directly interact with the UI.
  // The app will be woken up and handle the call when it resumes.
}

import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'dart:typed_data';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

/// Shows / cancels the "incoming call" notification.
///
/// On Android this is a high-priority, full-screen, vibrating notification in
/// the [AndroidNotificationCategory.call] category so it appears over the lock
/// screen and rings even when the app is backgrounded or has been killed.
/// Tapping it (or the OS full-screen launch) opens MainActivity, which
/// reconnects to the server and the queued call is delivered over the
/// WebSocket.
class CallNotifications {
  CallNotifications._();

  static final FlutterLocalNotificationsPlugin _plugin =
      FlutterLocalNotificationsPlugin();
  static bool _initialized = false;
  static bool _permissionRequested = false;

  static const _channelId = 'incoming_calls';
  static const _voicemailChannelId = 'voicemail';
  static const _messageChannelId = 'messages';

  /// Initialize from the app's main isolate and ask the user for notification
  /// permission (Android 13+). Should only be called when the UI is available.
  static Future<void> initialize({
    void Function(Map<String, String> call)? onCallTap,
  }) async {
    if (!Platform.isAndroid) return;
    await _ensureInit(onCallTap: onCallTap, requestPermission: true);
  }

  static Future<void> _ensureInit({
    void Function(Map<String, String> call)? onCallTap,
    bool requestPermission = false,
  }) async {
    if (!_initialized) {
      final androidInit = const AndroidInitializationSettings('ic_stat_ocn');
      final settings = InitializationSettings(
        android: androidInit,
        iOS: DarwinInitializationSettings(
          requestAlertPermission: false,
          requestBadgePermission: false,
          requestSoundPermission: false,
        ),
      );

      await _plugin.initialize(
        settings: settings,
        onDidReceiveNotificationResponse: (response) {
          final payload = response.payload;
          if (payload == null || payload.isEmpty) return;
          try {
            final data = jsonDecode(payload) as Map<String, dynamic>;
            onCallTap?.call(data.cast<String, String>());
          } catch (e) {
            log('CallNotifications: failed to parse payload: $e');
          }
        },
      );
      _initialized = true;
      log('CallNotifications: initialized');
    }

    if (requestPermission && !_permissionRequested) {
      _permissionRequested = true;
      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      final granted = await android?.requestNotificationsPermission();
      log('CallNotifications: notification permission granted=$granted');
    }
  }

  static int _idFor(String callId) => callId.hashCode & 0x7fffffff;

  static AndroidNotificationDetails get _details => AndroidNotificationDetails(
    _channelId,
    'Incoming calls',
    channelDescription: 'Rings when an incoming call arrives',
    importance: Importance.max,
    priority: Priority.max,
    category: AndroidNotificationCategory.call,
    fullScreenIntent: true,
    playSound: true,
    sound: const RawResourceAndroidNotificationSound('ring'),
    enableVibration: true,
    vibrationPattern: Int64List.fromList([0, 1000, 500, 1000, 500, 1000]),
    visibility: NotificationVisibility.public,
    autoCancel: false,
    ongoing: true,
    timeoutAfter: 45000,
    styleInformation: const BigTextStyleInformation(''),
  );

  /// Show the incoming-call notification. Safe to call from a background
  /// isolate (the FCM background handler) since each isolate has its own
  /// plugin instance; it lazily re-initializes without requesting permission.
  static Future<void> showIncomingCall({
    required String callId,
    required String callerNumber,
    required String callerName,
  }) async {
    if (!Platform.isAndroid) return;
    await _ensureInit();

    final title = callerName.isNotEmpty ? callerName : callerNumber;
    final body = 'Incoming call';

    final payload = jsonEncode({
      'type': 'incoming_call',
      'call_id': callId,
      'caller_number': callerNumber,
      'caller_name': callerName,
    });

    await _plugin.show(
      id: _idFor(callId),
      title: title,
      body: body,
      notificationDetails: NotificationDetails(android: _details),
      payload: payload,
    );
    log('CallNotifications: showing incoming call $callId from $callerNumber');
  }

  static Future<void> cancelIncomingCall(String callId) async {
    if (!Platform.isAndroid || !_initialized) return;
    await _plugin.cancel(id: _idFor(callId));
    log('CallNotifications: cancelled incoming call $callId');
  }

  /// Show a "new voicemail" notification. Safe from a background isolate.
  static Future<void> showVoicemail({
    required String callerNumber,
    required String callerName,
  }) async {
    if (!Platform.isAndroid) return;
    await _ensureInit();

    final title = 'New voicemail';
    final body = callerName.isNotEmpty ? callerName : callerNumber;

    final payload = jsonEncode({
      'type': 'voicemail',
      'caller_number': callerNumber,
      'caller_name': callerName,
    });

    // Unique id per message so several don't overwrite each other.
    final id = '$callerNumber${DateTime.now().microsecondsSinceEpoch}'
        .hashCode &
        0x7fffffff;

    const details = AndroidNotificationDetails(
      _voicemailChannelId,
      'Voicemail',
      channelDescription: 'New voicemail notifications',
      importance: Importance.high,
      priority: Priority.high,
      category: AndroidNotificationCategory.message,
      visibility: NotificationVisibility.public,
      styleInformation: BigTextStyleInformation(''),
    );

    await _plugin.show(
      id: id,
      title: title,
      body: body,
      notificationDetails: NotificationDetails(android: details),
      payload: payload,
    );
    log('CallNotifications: showing voicemail notification from $callerNumber');
  }

  /// Show a "new message" notification. Safe from a background isolate.
  static Future<void> showMessage({
    required String fromNumber,
    required String fromName,
  }) async {
    if (!Platform.isAndroid) return;
    await _ensureInit();

    final title = fromName.isNotEmpty ? fromName : fromNumber;
    final body = 'New message';

    final payload = jsonEncode({
      'type': 'message',
      'caller_number': fromNumber,
      'caller_name': fromName,
    });

    final id = '$fromNumber${DateTime.now().microsecondsSinceEpoch}'
        .hashCode &
        0x7fffffff;

    const details = AndroidNotificationDetails(
      _messageChannelId,
      'Messages',
      channelDescription: 'New message notifications',
      importance: Importance.high,
      priority: Priority.high,
      category: AndroidNotificationCategory.message,
      visibility: NotificationVisibility.public,
      styleInformation: BigTextStyleInformation(''),
    );

    await _plugin.show(
      id: id,
      title: title,
      body: body,
      notificationDetails: NotificationDetails(android: details),
      payload: payload,
    );
    log('CallNotifications: showing message notification from $fromNumber');
  }

  static Future<void> cancelAll() async {
    if (!Platform.isAndroid || !_initialized) return;
    await _plugin.cancelAll();
  }}

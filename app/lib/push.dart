import 'dart:async';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import 'api.dart';

/// Android notification channel for incoming pages: max importance, alarm
/// audio stream, vibration. Channel settings are fixed once created.
const _channel = AndroidNotificationChannel(
  'pages',
  'Incoming pages',
  description: 'Alerts that keep ringing until you acknowledge them',
  importance: Importance.max,
  playSound: true,
  enableVibration: true,
  audioAttributesUsage: AudioAttributesUsage.alarm,
);

/// FLAG_INSISTENT: repeat the sound until the notification is cancelled/opened.
const _flagInsistent = 4;

final FlutterLocalNotificationsPlugin _notifications = FlutterLocalNotificationsPlugin();

/// Page ids tapped/opened from a notification; the UI listens and navigates.
final StreamController<String> openedPages = StreamController<String>.broadcast();

int _notificationId(String pageId) => pageId.hashCode & 0x7fffffff;

Future<void> _initNotifications() async {
  await _notifications.initialize(
    const InitializationSettings(
      android: AndroidInitializationSettings('@mipmap/ic_launcher'),
    ),
    onDidReceiveNotificationResponse: (r) {
      if (r.payload != null) openedPages.add(r.payload!);
    },
  );
  await _notifications
      .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()
      ?.createNotificationChannel(_channel);
}

/// Shows the full-screen alarm for a page push. Safe in any isolate.
Future<void> _showAlarm(Map<String, dynamic> data) async {
  final pageId = data['page_id'] as String?;
  if (pageId == null) return;
  final sender = (data['sender_name'] as String?)?.trim();
  final attempt = int.tryParse('${data['attempt'] ?? ''}') ?? 0;
  await _notifications.show(
    _notificationId(pageId),
    sender == null || sender.isEmpty ? 'Incoming page' : 'Page from $sender',
    attempt > 0 ? 'Tap to open and acknowledge (reminder $attempt)' : 'Tap to open and acknowledge',
    NotificationDetails(
      android: AndroidNotificationDetails(
        _channel.id,
        _channel.name,
        channelDescription: _channel.description,
        importance: Importance.max,
        priority: Priority.max,
        category: AndroidNotificationCategory.alarm,
        fullScreenIntent: true,
        audioAttributesUsage: AudioAttributesUsage.alarm,
        additionalFlags: Int32List.fromList([_flagInsistent]),
        ongoing: true,
        autoCancel: false,
        visibility: NotificationVisibility.public,
      ),
    ),
    payload: pageId,
  );
}

Future<void> _onPush(RemoteMessage m) async {
  await _showAlarm(m.data);
  final id = m.data['page_id'] as String?;
  if (id != null) {
    try {
      await Api.instance.delivered(id);
    } catch (e) {
      debugPrint('delivered receipt failed: $e');
    }
  }
}

/// FCM handler for background/terminated state. Runs in its own isolate.
@pragma('vm:entry-point')
Future<void> firebaseBackgroundHandler(RemoteMessage message) async {
  await Firebase.initializeApp();
  await _initNotifications();
  await _onPush(message);
}

class Push {
  static bool _started = false;

  /// Call once at startup (before runApp).
  static Future<void> init() async {
    await Firebase.initializeApp();
    FirebaseMessaging.onBackgroundMessage(firebaseBackgroundHandler);
    await _initNotifications();
    FirebaseMessaging.onMessage.listen(_onPush);
  }

  /// If the app was launched by tapping (or full-screening) a page alarm.
  static Future<String?> launchPageId() async {
    final d = await _notifications.getNotificationAppLaunchDetails();
    if (d?.didNotificationLaunchApp ?? false) return d!.notificationResponse?.payload;
    return null;
  }

  /// After sign-in: ask for permissions and register this device's FCM token.
  static Future<void> registerDevice() async {
    final android = _notifications
        .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>();
    await android?.requestNotificationsPermission();
    await android?.requestFullScreenIntentPermission();

    final token = await FirebaseMessaging.instance.getToken();
    if (token != null) await Api.instance.registerDevice(token, 'android');
    if (!_started) {
      _started = true;
      FirebaseMessaging.instance.onTokenRefresh.listen((t) async {
        try {
          await Api.instance.registerDevice(t, 'android');
        } catch (e) {
          debugPrint('token refresh registration failed: $e');
        }
      });
    }
  }

  /// Stop the alarm for a page (after ack).
  static Future<void> dismiss(String pageId) => _notifications.cancel(_notificationId(pageId));
}

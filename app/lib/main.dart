import 'dart:async';

import 'package:flutter/material.dart';

import 'auth.dart';
import 'push.dart';
import 'screens/auth_screen.dart';
import 'screens/home_screen.dart';
import 'screens/incoming_screen.dart';

final navigatorKey = GlobalKey<NavigatorState>();

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await Push.init();
  final signedIn = await Auth.instance.restore();
  final launchPage = await Push.launchPageId();
  runApp(AlertMeApp(signedIn: signedIn, launchPageId: launchPage));
}

class AlertMeApp extends StatefulWidget {
  const AlertMeApp({super.key, required this.signedIn, this.launchPageId});
  final bool signedIn;
  final String? launchPageId;

  @override
  State<AlertMeApp> createState() => _AlertMeAppState();
}

class _AlertMeAppState extends State<AlertMeApp> {
  late bool _signedIn = widget.signedIn;
  StreamSubscription<String>? _opened;

  @override
  void initState() {
    super.initState();
    _opened = openedPages.stream.listen(_showIncoming);
    if (_signedIn) {
      _afterSignIn();
      if (widget.launchPageId != null) {
        WidgetsBinding.instance.addPostFrameCallback((_) => _showIncoming(widget.launchPageId!));
      }
    }
  }

  @override
  void dispose() {
    _opened?.cancel();
    super.dispose();
  }

  void _showIncoming(String pageId) {
    if (!_signedIn) return;
    navigatorKey.currentState?.push(
      MaterialPageRoute(builder: (_) => IncomingScreen(pageId: pageId)),
    );
  }

  Future<void> _afterSignIn() async {
    try {
      await Push.registerDevice();
    } catch (e) {
      final ctx = navigatorKey.currentContext;
      if (ctx != null && ctx.mounted) {
        ScaffoldMessenger.of(ctx).showSnackBar(
            SnackBar(content: Text('Push registration failed: $e')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'AlertMe',
      navigatorKey: navigatorKey,
      theme: ThemeData(colorSchemeSeed: Colors.deepOrange, useMaterial3: true),
      darkTheme: ThemeData(
          colorSchemeSeed: Colors.deepOrange, brightness: Brightness.dark, useMaterial3: true),
      home: _signedIn
          ? HomeScreen(onSignOut: () async {
              await Auth.instance.signOut();
              setState(() => _signedIn = false);
            })
          : AuthScreen(onSignedIn: () {
              setState(() => _signedIn = true);
              _afterSignIn();
            }),
    );
  }
}

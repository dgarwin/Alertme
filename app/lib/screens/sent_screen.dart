import 'dart:async';

import 'package:flutter/material.dart' hide Page;

import '../api.dart';
import '../models.dart';

/// A page I sent: its message and a live-ish timeline (polls every 3s while
/// the page is still open).
class SentScreen extends StatefulWidget {
  const SentScreen({super.key, required this.pageId, this.peerName});
  final String pageId;
  final String? peerName;

  @override
  State<SentScreen> createState() => _SentScreenState();
}

class _SentScreenState extends State<SentScreen> {
  Page? _page;
  List<PageEvent> _events = [];
  String? _error;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    _load();
    _poll = Timer.periodic(const Duration(seconds: 3), (_) => _load());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final (page, events) = await Api.instance.page(widget.pageId);
      if (!page.isOpen) _poll?.cancel();
      if (mounted) {
        setState(() {
          _page = page;
          _events = events..sort((a, b) => a.at.compareTo(b.at));
          _error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e.toString());
    }
  }

  static const _labels = {
    'created': ('Sent', Icons.send),
    'push_attempt': ('Alert pushed', Icons.notifications),
    'delivered': ('Delivered to phone', Icons.phone_android),
    'seen': ('Seen', Icons.visibility),
    'acked': ('Acknowledged', Icons.check_circle),
    'expired': ('Expired', Icons.timer_off),
    'cancelled': ('Cancelled', Icons.cancel),
    'no_devices': ('Recipient has no registered phone', Icons.phonelink_erase),
  };

  @override
  Widget build(BuildContext context) {
    final page = _page;
    return Scaffold(
      appBar: AppBar(title: Text('To ${widget.peerName ?? '…'}')),
      body: page == null
          ? Center(child: _error != null ? Text(_error!) : const CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(page.message, style: Theme.of(context).textTheme.titleMedium),
                  ),
                ),
                const SizedBox(height: 8),
                ListTile(
                  title: Text('Status: ${page.state}'),
                  subtitle: page.ackNote != null ? Text('“${page.ackNote}”') : null,
                  trailing: page.isOpen
                      ? const SizedBox(
                          width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                      : null,
                ),
                const Divider(),
                for (final e in _events)
                  ListTile(
                    dense: true,
                    leading: Icon(_labels[e.type]?.$2 ?? Icons.circle, size: 20),
                    title: Text(_labels[e.type]?.$1 ?? e.type),
                    subtitle: e.detail == null ? null : Text(e.detail!),
                    trailing: Text(TimeOfDay.fromDateTime(e.at).format(context)),
                  ),
              ],
            ),
    );
  }
}

import 'package:flutter/material.dart' hide Page;

import '../api.dart';
import '../models.dart';
import '../push.dart';

/// The alarm screen: who's paging, the message, and a big ACK button.
class IncomingScreen extends StatefulWidget {
  const IncomingScreen({super.key, required this.pageId});
  final String pageId;

  @override
  State<IncomingScreen> createState() => _IncomingScreenState();
}

class _IncomingScreenState extends State<IncomingScreen> {
  Page? _page;
  String _sender = '';
  String? _error;
  bool _busy = false;
  final _note = TextEditingController();

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final (page, _) = await Api.instance.page(widget.pageId);
      final pairings = await Api.instance.pairings();
      final name = pairings
          .where((p) => p.peerId == page.senderId)
          .map((p) => p.peerName)
          .firstOrNull;
      if (!page.isOpen) await Push.dismiss(widget.pageId);
      if (mounted) {
        setState(() {
          _page = page;
          _sender = name ?? 'Someone';
        });
      }
    } catch (e) {
      if (mounted) setState(() => _error = e.toString());
    }
  }

  Future<void> _ack([String? note]) async {
    setState(() => _busy = true);
    try {
      final page = await Api.instance.ack(widget.pageId, note: note ?? _note.text.trim());
      await Push.dismiss(widget.pageId);
      if (mounted) setState(() => _page = page);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Ack failed: $e')));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final page = _page;
    final open = page?.isOpen ?? false;
    return Scaffold(
      backgroundColor: open ? cs.errorContainer : null,
      appBar: AppBar(
        title: const Text('Incoming page'),
        backgroundColor: open ? cs.errorContainer : null,
      ),
      body: SafeArea(
        child: page == null
            ? Center(
                child: _error != null
                    ? Padding(padding: const EdgeInsets.all(24), child: Text(_error!))
                    : const CircularProgressIndicator())
            : ListView(
                padding: const EdgeInsets.all(24),
                children: [
                  Icon(open ? Icons.notifications_active : Icons.check_circle,
                      size: 72, color: open ? cs.error : Colors.green),
                  const SizedBox(height: 16),
                  Text(_sender,
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.headlineLarge
                          ?.copyWith(fontWeight: FontWeight.bold)),
                  const SizedBox(height: 16),
                  Card(
                    child: Padding(
                      padding: const EdgeInsets.all(20),
                      child: Text(page.message,
                          style: Theme.of(context).textTheme.titleLarge),
                    ),
                  ),
                  const SizedBox(height: 24),
                  if (open) ...[
                    TextField(
                      controller: _note,
                      decoration: const InputDecoration(
                        labelText: 'Reply note (optional)',
                        filled: true,
                      ),
                    ),
                    const SizedBox(height: 12),
                    Wrap(spacing: 8, children: [
                      for (final q in ['On my way', 'Calling you now', 'Got it'])
                        ActionChip(label: Text(q), onPressed: _busy ? null : () => _ack(q)),
                    ]),
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: _busy ? null : _ack,
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(72),
                        textStyle: const TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
                      ),
                      child: Text(_busy ? 'Acknowledging…' : 'ACKNOWLEDGE'),
                    ),
                  ] else
                    Text(
                      page.state == 'acked'
                          ? 'Acknowledged${page.ackNote != null ? ' — “${page.ackNote}”' : ''}'
                          : 'This page is ${page.state}.',
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                ],
              ),
      ),
    );
  }
}

import 'package:flutter/material.dart' hide Page;

import '../api.dart';
import '../models.dart';

/// Pick an active contact, type a message, send. Pops with the created Page.
class SendScreen extends StatefulWidget {
  const SendScreen({super.key, required this.contacts});
  final List<Pairing> contacts;

  @override
  State<SendScreen> createState() => _SendScreenState();
}

class _SendScreenState extends State<SendScreen> {
  late String _to = widget.contacts.first.peerId;
  final _message = TextEditingController();
  bool _busy = false;
  String? _error;

  Future<void> _send() async {
    final text = _message.text.trim();
    if (text.isEmpty) {
      setState(() => _error = 'Type a message.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final page = await Api.instance.sendPage(_to, text);
      if (mounted) Navigator.pop<Page>(context, page);
    } on ApiException catch (e) {
      setState(() => _error = e.status == 403 ? 'You are not paired with this contact.' : e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Send a page')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          DropdownButtonFormField<String>(
            initialValue: _to,
            decoration: const InputDecoration(labelText: 'To'),
            items: [
              for (final c in widget.contacts)
                DropdownMenuItem(value: c.peerId, child: Text(c.peerName)),
            ],
            onChanged: (v) => setState(() => _to = v!),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _message,
            autofocus: true,
            maxLength: 500,
            maxLines: 5,
            minLines: 2,
            decoration: const InputDecoration(
              labelText: 'Message',
              hintText: 'e.g. Call me now — urgent',
              border: OutlineInputBorder(),
            ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(_error!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
            ),
          const SizedBox(height: 8),
          FilledButton.icon(
            onPressed: _busy ? null : _send,
            icon: const Icon(Icons.campaign),
            label: Text(_busy ? 'Sending…' : 'Send page'),
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(52)),
          ),
          const SizedBox(height: 12),
          Text(
            'Their phone will keep alerting until they acknowledge.',
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
    );
  }
}

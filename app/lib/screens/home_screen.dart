import 'dart:async';

import 'package:flutter/material.dart' hide Page;
import 'package:flutter/services.dart';

import '../api.dart';
import '../auth.dart';
import '../models.dart';
import 'incoming_screen.dart';
import 'send_screen.dart';
import 'sent_screen.dart';

String timeAgo(DateTime t) {
  final d = DateTime.now().difference(t);
  if (d.inSeconds < 60) return 'just now';
  if (d.inMinutes < 60) return '${d.inMinutes}m ago';
  if (d.inHours < 24) return '${d.inHours}h ago';
  return '${d.inDays}d ago';
}

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.onSignOut});
  final VoidCallback onSignOut;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> with WidgetsBindingObserver {
  List<Page> _pages = [];
  List<Pairing> _pairings = [];
  String? _error;
  bool _loading = true;
  Timer? _poll;

  Map<String, String> get _names => {for (final p in _pairings) p.peerId: p.peerName};

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _refresh();
    _startPolling();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _poll?.cancel();
    super.dispose();
  }

  // Poll while the app is in the foreground so a missed push still shows up.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _refresh();
      _startPolling();
    } else if (state == AppLifecycleState.paused) {
      _poll?.cancel();
    }
  }

  void _startPolling() {
    _poll?.cancel();
    _poll = Timer.periodic(const Duration(seconds: 10), (_) => _refresh());
  }

  Future<void> _refresh() async {
    try {
      final results = await Future.wait([Api.instance.pages(), Api.instance.pairings()]);
      if (!mounted) return;
      setState(() {
        _pages = (results[0] as List<Page>)..sort((a, b) => b.createdAt.compareTo(a.createdAt));
        _pairings = results[1] as List<Pairing>;
        _error = null;
        _loading = false;
      });
    } catch (e) {
      if (mounted) {
        setState(() {
          _error = e.toString();
          _loading = false;
        });
      }
    }
  }

  Future<void> _open(Page p) async {
    final mine = p.senderId == Auth.instance.userId;
    await Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => mine
          ? SentScreen(pageId: p.id, peerName: _names[p.recipientId])
          : IncomingScreen(pageId: p.id),
    ));
    _refresh();
  }

  Future<void> _send() async {
    final active = _pairings.where((p) => p.isActive).toList();
    if (active.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Add a contact first (Contacts tab).')));
      return;
    }
    final sent = await Navigator.of(context).push<Page>(
        MaterialPageRoute(builder: (_) => SendScreen(contacts: active)));
    if (sent != null && mounted) {
      await _refresh();
      if (mounted) _open(sent);
    }
  }

  @override
  Widget build(BuildContext context) {
    return DefaultTabController(
      length: 2,
      child: Scaffold(
        appBar: AppBar(
          title: const Text('AlertMe'),
          actions: [
            PopupMenuButton<String>(
              onSelected: (v) {
                if (v == 'out') widget.onSignOut();
              },
              itemBuilder: (_) => [
                PopupMenuItem(enabled: false, child: Text(Auth.instance.email ?? '')),
                const PopupMenuItem(value: 'out', child: Text('Sign out')),
              ],
            ),
          ],
          bottom: const TabBar(tabs: [Tab(text: 'Activity'), Tab(text: 'Contacts')]),
        ),
        floatingActionButton: FloatingActionButton.extended(
          onPressed: _send,
          icon: const Icon(Icons.campaign),
          label: const Text('Page'),
        ),
        body: _loading
            ? const Center(child: CircularProgressIndicator())
            : Column(
                children: [
                  if (_error != null)
                    MaterialBanner(
                      content: Text(_error!),
                      actions: [TextButton(onPressed: _refresh, child: const Text('Retry'))],
                    ),
                  Expanded(
                    child: TabBarView(children: [
                      _activity(),
                      _ContactsTab(pairings: _pairings, onChanged: _refresh),
                    ]),
                  ),
                ],
              ),
      ),
    );
  }

  Widget _activity() {
    if (_pages.isEmpty) {
      return RefreshIndicator(
        onRefresh: _refresh,
        child: ListView(children: const [
          SizedBox(height: 120),
          Center(child: Text('No pages yet.')),
        ]),
      );
    }
    final me = Auth.instance.userId;
    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView.separated(
        padding: const EdgeInsets.only(bottom: 96),
        itemCount: _pages.length,
        separatorBuilder: (_, _) => const Divider(height: 1),
        itemBuilder: (_, i) {
          final p = _pages[i];
          final outgoing = p.senderId == me;
          final peer = _names[outgoing ? p.recipientId : p.senderId] ?? 'Unknown';
          final urgent = !outgoing && p.isOpen;
          return ListTile(
            leading: Icon(outgoing ? Icons.call_made : Icons.call_received,
                color: urgent ? Theme.of(context).colorScheme.error : null),
            title: Text(outgoing ? 'To $peer' : 'From $peer'),
            subtitle: Text(p.message, maxLines: 2, overflow: TextOverflow.ellipsis),
            trailing: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                _StateChip(state: p.state),
                const SizedBox(height: 4),
                Text(timeAgo(p.createdAt), style: Theme.of(context).textTheme.bodySmall),
              ],
            ),
            onTap: () => _open(p),
          );
        },
      ),
    );
  }
}

class _StateChip extends StatelessWidget {
  const _StateChip({required this.state});
  final String state;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final (bg, fg) = switch (state) {
      'acked' => (Colors.green.shade100, Colors.green.shade900),
      'expired' || 'cancelled' => (cs.surfaceContainerHighest, cs.onSurfaceVariant),
      _ => (cs.errorContainer, cs.onErrorContainer),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(12)),
      child: Text(state, style: TextStyle(color: fg, fontSize: 12)),
    );
  }
}

class _ContactsTab extends StatelessWidget {
  const _ContactsTab({required this.pairings, required this.onChanged});
  final List<Pairing> pairings;
  final Future<void> Function() onChanged;

  Future<void> _createInvite(BuildContext context) async {
    try {
      final inv = await Api.instance.createInvite();
      if (!context.mounted) return;
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Invite code'),
          content: Column(mainAxisSize: MainAxisSize.min, children: [
            const Text('Give this code to the person you want to pair with:'),
            const SizedBox(height: 16),
            SelectableText(inv.code,
                style: Theme.of(ctx).textTheme.headlineMedium
                    ?.copyWith(letterSpacing: 4, fontWeight: FontWeight.bold)),
            const SizedBox(height: 8),
            Text('Expires ${TimeOfDay.fromDateTime(inv.expiresAt).format(ctx)}',
                style: Theme.of(ctx).textTheme.bodySmall),
          ]),
          actions: [
            TextButton(
              onPressed: () => Clipboard.setData(ClipboardData(text: inv.code)),
              child: const Text('Copy'),
            ),
            FilledButton(onPressed: () => Navigator.pop(ctx), child: const Text('Done')),
          ],
        ),
      );
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
      }
    }
  }

  Future<void> _acceptInvite(BuildContext context) async {
    final code = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Enter invite code'),
        content: TextField(
          controller: code,
          autofocus: true,
          textCapitalization: TextCapitalization.characters,
          decoration: const InputDecoration(labelText: 'Code'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Pair')),
        ],
      ),
    );
    if (ok != true || code.text.trim().isEmpty) return;
    try {
      final p = await Api.instance.acceptInvite(code.text);
      if (!context.mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('Paired with ${p.peerName}')));
      await onChanged();
    } on ApiException catch (e) {
      if (!context.mounted) return;
      final msg = switch (e.status) {
        404 => 'That code is invalid or expired.',
        400 => "You can't pair with yourself.",
        _ => e.message,
      };
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(msg)));
    }
  }

  Future<void> _block(BuildContext context, Pairing p) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Block ${p.peerName}?'),
        content: const Text("They won't be able to page you."),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Block')),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await Api.instance.block(p.peerId);
      await onChanged();
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.only(bottom: 96),
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: Row(children: [
            Expanded(
              child: OutlinedButton.icon(
                onPressed: () => _createInvite(context),
                icon: const Icon(Icons.qr_code),
                label: const Text('Create invite'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: OutlinedButton.icon(
                onPressed: () => _acceptInvite(context),
                icon: const Icon(Icons.person_add),
                label: const Text('Enter code'),
              ),
            ),
          ]),
        ),
        if (pairings.isEmpty)
          const Padding(
            padding: EdgeInsets.all(32),
            child: Text('No contacts yet. Create an invite and share the code, '
                'or enter a code someone gave you.',
                textAlign: TextAlign.center),
          ),
        for (final p in pairings)
          ListTile(
            leading: CircleAvatar(
                child: Text(p.peerName.isEmpty ? '?' : p.peerName[0].toUpperCase())),
            title: Text(p.peerName),
            subtitle: Text(p.isActive ? 'Paired ${timeAgo(p.createdAt)}' : 'Blocked'),
            trailing: p.isActive
                ? IconButton(
                    tooltip: 'Block',
                    icon: const Icon(Icons.block),
                    onPressed: () => _block(context, p),
                  )
                : null,
          ),
      ],
    );
  }
}

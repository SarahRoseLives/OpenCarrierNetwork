import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/db/dm_message.dart';
import '../../core/phone/number_format.dart';
import 'thread_screen.dart';

class MessagesScreen extends StatelessWidget {
  const MessagesScreen({super.key});

  Future<void> _startNewMessage(BuildContext context) async {
    final appState = context.read<AppState>();
    final controller = TextEditingController();
    final number = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('New message'),
        content: TextField(
          controller: controller,
          autofocus: true,
          keyboardType: TextInputType.phone,
          decoration: const InputDecoration(
            labelText: 'OCN number',
            hintText: '5551234 or 4405551234',
            border: OutlineInputBorder(),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('Start'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (number == null || number.isEmpty || !context.mounted) return;
    final canon = canonicalNumber(
      number,
      ownArea: appState.phoneNumber?.areaCode,
    );
    if (canon.isEmpty) return;
    Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => ThreadScreen(peer: canon)),
    );
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final threads = appState.dmThreads;

    return Scaffold(
      appBar: AppBar(title: const Text('Messages')),
      floatingActionButton: FloatingActionButton(
        heroTag: 'messages_new_message',
        tooltip: 'New message',
        onPressed: () => _startNewMessage(context),
        child: const Icon(Icons.edit_outlined),
      ),
      body: threads.isEmpty
          ? Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.chat_bubble_outline, size: 64, color: Colors.grey[400]),
                  const SizedBox(height: 12),
                  Text('No conversations', style: TextStyle(color: Colors.grey[600])),
                  const SizedBox(height: 4),
                  Text(
                    'Tap + to start a message to another OCN number.',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Colors.grey[500], fontSize: 13),
                  ),
                ],
              ),
            )
          : ListView.separated(
              itemCount: threads.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (context, i) => _ThreadRow(thread: threads[i]),
            ),
    );
  }
}

class _ThreadRow extends StatelessWidget {
  final DmThread thread;
  const _ThreadRow({required this.thread});

  String _preview() {
    final m = thread.lastMessage;
    if (m.isImage) return '[Image]';
    final text = m.text.trim();
    return text.isEmpty ? 'No text' : text;
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.read<AppState>();
    final peer = thread.peer;
    final contact = appState.contactForNumber(peer);
    final title = contact?.name ?? displayNumber(peer);
    final initial = title.trim().isEmpty ? '?' : title.trim()[0].toUpperCase();

    return ListTile(
      leading: CircleAvatar(child: Text(initial)),
      title: Text(
        title,
        style: TextStyle(
          fontWeight: thread.unread > 0 ? FontWeight.w600 : FontWeight.normal,
        ),
      ),
      subtitle: Text(
        _preview(),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      trailing: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Text(_clock(thread.lastMessage.createdAt),
              style: TextStyle(color: Colors.grey[600], fontSize: 12)),
          if (thread.unread > 0)
            Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Badge.count(count: thread.unread),
            ),
        ],
      ),
      onTap: () {
        Navigator.of(context).push(
          MaterialPageRoute(builder: (_) => ThreadScreen(peer: peer)),
        );
      },
    );
  }
}

String _clock(DateTime t) {
  final h = t.hour.toString().padLeft(2, '0');
  final m = t.minute.toString().padLeft(2, '0');
  return '$h:$m';
}

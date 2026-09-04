import 'dart:io';
import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import 'package:provider/provider.dart';
import '../../core/app_state.dart';
import '../../core/db/dm_message.dart';
import '../../core/phone/number_format.dart';

class ThreadScreen extends StatefulWidget {
  final String peer;
  const ThreadScreen({super.key, required this.peer});

  @override
  State<ThreadScreen> createState() => _ThreadScreenState();
}

class _ThreadScreenState extends State<ThreadScreen> {
  final TextEditingController _composer = TextEditingController();
  final ScrollController _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      context.read<AppState>().openThread(widget.peer);
      _scrollToBottom(instant: true);
    });
  }

  @override
  void dispose() {
    _composer.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _scrollToBottom({bool instant = false}) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: instant ? Duration.zero : const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    });
  }

  Future<void> _sendText() async {
    final text = _composer.text.trim();
    if (text.isEmpty) return;
    _composer.clear();
    await context.read<AppState>().sendTextMessage(widget.peer, text);
    _scrollToBottom();
  }

  Future<void> _pickImage() async {
    final appState = context.read<AppState>();
    try {
      final picked = await ImagePicker().pickImage(source: ImageSource.gallery);
      if (picked == null) return;
      final bytes = await picked.readAsBytes();
      if (bytes.length > 4 * 1024 * 1024) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('Image too large (max 4 MB).')),
          );
        }
        return;
      }
      final mime = _mimeForName(picked.name);
      await appState.sendImageMessage(
        peer: widget.peer,
        bytes: bytes,
        mime: mime,
        name: picked.name,
      );
      _scrollToBottom();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not attach image: $e')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final appState = context.watch<AppState>();
    final messages = appState.messagesFor(widget.peer);
    final contact = appState.contactForNumber(widget.peer);
    final title = contact?.name ?? displayNumber(widget.peer);

    return Scaffold(
      appBar: AppBar(title: Text(title)),
      body: Column(
        children: [
          Expanded(
            child: messages.isEmpty
                ? Center(
                    child: Text(
                      'Say hello to ${displayNumber(widget.peer)}',
                      style: TextStyle(color: Colors.grey[500]),
                    ),
                  )
                : ListView.builder(
                    controller: _scroll,
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 8,
                    ),
                    itemCount: messages.length,
                    itemBuilder: (context, i) =>
                        _Bubble(message: messages[i], appState: appState),
                  ),
          ),
          _Composer(
            controller: _composer,
            onSend: _sendText,
            onAttach: _pickImage,
          ),
        ],
      ),
    );
  }
}

class _Bubble extends StatefulWidget {
  final DmMessage message;
  final AppState appState;
  const _Bubble({required this.message, required this.appState});

  @override
  State<_Bubble> createState() => _BubbleState();
}

class _BubbleState extends State<_Bubble> {
  late Future<String?> _path;

  @override
  void initState() {
    super.initState();
    _path = _resolve();
  }

  Future<String?> _resolve() {
    final appState = widget.appState;
    final m = widget.message;
    final existing = m.imagePath;
    if (existing != null && existing.isNotEmpty) return Future.value(existing);
    return appState.imagePathFor(m);
  }

  @override
  Widget build(BuildContext context) {
    final m = widget.message;
    final outbound = !m.isInbound;

    Widget child;
    if (m.isImage) {
      child = FutureBuilder<String?>(
        future: _path,
        builder: (context, snap) {
          if (snap.connectionState != ConnectionState.done ||
              snap.data == null) {
            return const SizedBox(
              width: 180,
              height: 120,
              child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
            );
          }
          return GestureDetector(
            onTap: () => Navigator.of(context).push(
              MaterialPageRoute(
                builder: (_) => Scaffold(
                  backgroundColor: Colors.black,
                  appBar: AppBar(backgroundColor: Colors.black),
                  body: Center(
                    child: InteractiveViewer(
                      child: Image.file(File(snap.data!), fit: BoxFit.contain),
                    ),
                  ),
                ),
              ),
            ),
            child: ClipRRect(
              borderRadius: BorderRadius.circular(8),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 220),
                child: Image.file(
                  File(snap.data!),
                  fit: BoxFit.cover,
                ),
              ),
            ),
          );
        },
      );
    } else {
      child = Text(m.text, style: const TextStyle(fontSize: 15));
    }

    final color = outbound ? Colors.blue[100] : Colors.grey[300];

    return Align(
      alignment: outbound ? Alignment.centerRight : Alignment.centerLeft,
      child: Column(
        crossAxisAlignment: outbound
            ? CrossAxisAlignment.end
            : CrossAxisAlignment.start,
        children: [
          Container(
            margin: const EdgeInsets.symmetric(vertical: 3),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            constraints: const BoxConstraints(maxWidth: 280),
            decoration: BoxDecoration(
              color: color,
              borderRadius: BorderRadius.only(
                topLeft: const Radius.circular(14),
                topRight: const Radius.circular(14),
                bottomLeft: Radius.circular(outbound ? 14 : 2),
                bottomRight: Radius.circular(outbound ? 2 : 14),
              ),
            ),
            child: child,
          ),
          Padding(
            padding: const EdgeInsets.only(left: 6, right: 6),
            child: Text(
              _meta(m),
              style: TextStyle(color: Colors.grey[500], fontSize: 10),
            ),
          ),
        ],
      ),
    );
  }

  String _meta(DmMessage m) {
    final h = m.createdAt.hour.toString().padLeft(2, '0');
    final min = m.createdAt.minute.toString().padLeft(2, '0');
    final base = '$h:$min';
    if (m.isInbound) return base;
    switch (m.status) {
      case 'sending':
        return '$base · sending…';
      case 'failed':
        return '$base · not sent';
      default:
        return '$base · delivered';
    }
  }
}

class _Composer extends StatelessWidget {
  final TextEditingController controller;
  final VoidCallback onSend;
  final VoidCallback onAttach;

  const _Composer({
    required this.controller,
    required this.onSend,
    required this.onAttach,
  });

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.all(8),
        child: Row(
          children: [
            IconButton(
              icon: const Icon(Icons.add_photo_alternate_outlined),
              onPressed: onAttach,
              tooltip: 'Send image',
            ),
            Expanded(
              child: TextField(
                controller: controller,
                textInputAction: TextInputAction.send,
                onSubmitted: (_) => onSend(),
                minLines: 1,
                maxLines: 4,
                decoration: InputDecoration(
                  hintText: 'Text message',
                  isDense: true,
                  filled: true,
                  contentPadding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 10,
                  ),
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(24),
                    borderSide: BorderSide.none,
                  ),
                ),
              ),
            ),
            const SizedBox(width: 6),
            IconButton.filled(
              icon: const Icon(Icons.send),
              onPressed: onSend,
              tooltip: 'Send',
            ),
          ],
        ),
      ),
    );
  }
}

String _mimeForName(String name) {
  final lower = name.toLowerCase();
  if (lower.endsWith('.png')) return 'image/png';
  if (lower.endsWith('.gif')) return 'image/gif';
  if (lower.endsWith('.webp')) return 'image/webp';
  return 'image/jpeg';
}

import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'package:path/path.dart' as p;
import 'dm_message.dart';
import 'storage_paths.dart';

/// Local store of the user's 1:1 conversations. Messages are the source of
/// truth on the device; the server only relays/queues what isn't delivered.
class DmStore {
  /// Injectable for tests; defaults to the app data directory.
  DmStore({Future<String> Function()? baseDir})
    : _baseDir = baseDir ?? appDataBaseDir;

  final Future<String> Function() _baseDir;
  List<DmMessage> _items = [];
  bool _loaded = false;

  List<DmMessage> get items => List.unmodifiable(_items);

  Future<String> _path() async => p.join(await _baseDir(), 'ocn_dm.json');

  Future<String> _attachmentDir() async {
    final dir = Directory(p.join(await _baseDir(), 'dm_attachments'));
    if (!await dir.exists()) {
      await dir.create(recursive: true);
    }
    return dir.path;
  }

  /// Local filesystem path where an attachment for [messageId] is cached.
  Future<String> attachmentPathFor(String messageId, {String ext = 'img'}) async {
    return p.join(await _attachmentDir(), '$messageId.$ext');
  }

  Future<void> ensureLoaded() async {
    if (_loaded) return;
    _loaded = true;
    try {
      final file = File(await _path());
      if (!await file.exists()) return;
      final data = await file.readAsString();
      if (data.trim().isEmpty) return;
      final raw = jsonDecode(data) as List<dynamic>;
      _items = raw
          .map((e) => DmMessage.fromJson((e as Map).cast<String, dynamic>()))
          .toList();
      log('DmStore: loaded ${_items.length} messages');
    } catch (e) {
      log('DmStore load error: $e');
    }
  }

  DmMessage? byId(String id) {
    for (final m in _items) {
      if (m.id == id) return m;
    }
    return null;
  }

  DmMessage? byClientId(String clientId) {
    for (final m in _items) {
      if (m.clientId == clientId) return m;
    }
    return null;
  }

  /// Returns false when the message already existed (duplicate delivery).
  bool addOrUpdate(DmMessage message) {
    final existing = byId(message.id);
    if (existing != null) {
      final i = _items.indexOf(existing);
      _items[i] = message;
      return false;
    }
    _items.add(message);
    return true;
  }

  List<DmMessage> messagesFor(String peer) {
    final msgs = _items.where((m) => m.peer == peer).toList()
      ..sort((a, b) => a.createdAt.compareTo(b.createdAt));
    return msgs;
  }

  int unreadFor(String peer) =>
      _items.where((m) => m.peer == peer && m.isInbound && !m.read).length;

  int get unreadTotal =>
      _items.where((m) => m.isInbound && !m.read).length;

  void markPeerRead(String peer) {
    for (var i = 0; i < _items.length; i++) {
      final m = _items[i];
      if (m.peer == peer && m.isInbound && !m.read) {
        _items[i] = m.copyWith(read: true);
      }
    }
  }

  void removeById(String id) {
    _items.removeWhere((m) => m.id == id);
  }

  void removePeer(String peer) {
    _items.removeWhere((m) => m.peer == peer);
  }

  /// Conversations sorted by most recent activity.
  List<DmThread> get threads {
    final byPeer = <String, DmMessage>{};
    for (final m in _items) {
      final existing = byPeer[m.peer];
      if (existing == null || m.createdAt.isAfter(existing.createdAt)) {
        byPeer[m.peer] = m;
      }
    }
    final threads = byPeer.entries
        .map(
          (e) => DmThread(
            peer: e.key,
            lastMessage: e.value,
            unread: unreadFor(e.key),
          ),
        )
        .toList()
      ..sort((a, b) => b.lastMessage.createdAt.compareTo(a.lastMessage.createdAt));
    return threads;
  }

  Future<void> persist() async {
    try {
      final file = File(await _path());
      await file.parent.create(recursive: true);
      await file.writeAsString(
        jsonEncode(_items.map((m) => m.toJson()).toList()),
      );
    } catch (e) {
      log('DmStore persist error: $e');
    }
  }
}

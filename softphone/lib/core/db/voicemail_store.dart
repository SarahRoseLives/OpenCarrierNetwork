import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'package:path/path.dart' as p;
import 'voicemail_message.dart';
import 'storage_paths.dart';

/// Offline cache of the server's voicemail list. The server is the source of
/// truth; this just makes the list instantly available between refreshes.
class VoicemailStore {
  /// Injectable for tests; defaults to the app data directory.
  VoicemailStore({Future<String> Function()? baseDir})
    : _baseDir = baseDir ?? appDataBaseDir;

  final Future<String> Function() _baseDir;
  List<VoicemailMessage> _items = [];
  bool _loaded = false;

  List<VoicemailMessage> get items => List.unmodifiable(_items);

  int get unread => _items.where((m) => !m.listened).length;

  Future<String> _path() async =>
      p.join(await _baseDir(), 'ocn_voicemail.json');

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
          .map((e) => VoicemailMessage.fromJson((e as Map).cast<String, dynamic>()))
          .toList();
      log('VoicemailStore: loaded ${_items.length} messages');
    } catch (e) {
      log('VoicemailStore load error: $e');
    }
  }

  void replaceAll(List<VoicemailMessage> messages) {
    _items = [...messages];
  }

  VoicemailMessage? byId(String id) {
    for (final m in _items) {
      if (m.id == id) return m;
    }
    return null;
  }

  void upsert(VoicemailMessage message) {
    final i = _items.indexWhere((m) => m.id == message.id);
    if (i >= 0) {
      _items[i] = message;
    } else {
      _items.insert(0, message);
    }
  }

  void remove(String id) {
    _items.removeWhere((m) => m.id == id);
  }

  void markRead(String id) {
    final m = byId(id);
    if (m != null && !m.listened) {
      m.listened = true;
    }
  }

  Future<void> persist() async {
    try {
      final file = File(await _path());
      await file.parent.create(recursive: true);
      await file.writeAsString(
        jsonEncode(_items.map((m) => m.toJson()).toList()),
      );
    } catch (e) {
      log('VoicemailStore persist error: $e');
    }
  }
}

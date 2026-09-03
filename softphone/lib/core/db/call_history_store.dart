import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'package:path/path.dart' as p;
import 'call_log_entry.dart';
import 'storage_paths.dart';

/// Persistent call history backed by a JSON file, newest first.
///
/// Mutations update the in-memory list immediately and serialize a disk write
/// afterwards, so the UI never has to block on IO.
class CallHistoryStore {
  /// Injectable for tests; defaults to the app data directory.
  CallHistoryStore({Future<String> Function()? baseDir})
    : _baseDir = baseDir ?? appDataBaseDir;

  final Future<String> Function() _baseDir;

  static const int maxEntries = 300;

  List<CallLogEntry> _entries = [];
  bool _loaded = false;

  /// Newest first.
  List<CallLogEntry> get items {
    final sorted = [..._entries]
      ..sort((a, b) => b.startedAt.compareTo(a.startedAt));
    return List.unmodifiable(sorted);
  }

  Future<String> _path() async =>
      p.join(await _baseDir(), 'ocn_call_history.json');

  Future<void> ensureLoaded() async {
    if (_loaded) return;
    _loaded = true;
    try {
      final file = File(await _path());
      if (!await file.exists()) return;
      final data = await file.readAsString();
      if (data.trim().isEmpty) return;
      final raw = jsonDecode(data) as List<dynamic>;
      _entries = raw
          .map((e) => CallLogEntry.fromJson((e as Map).cast<String, dynamic>()))
          .toList();
      log('CallHistoryStore: loaded ${_entries.length} entries');
    } catch (e) {
      log('CallHistoryStore load error: $e');
    }
  }

  CallLogEntry? find(String id) {
    for (final e in _entries) {
      if (e.id == id) return e;
    }
    return null;
  }

  Future<void> update(String id, void Function(CallLogEntry entry) mutate) async {
    await ensureLoaded();
    final entry = find(id);
    if (entry == null) return;
    mutate(entry);
    await _persist();
  }

  Future<void> add(CallLogEntry entry) async {
    await ensureLoaded();
    _entries.add(entry);
    _prune();
    await _persist();
  }

  Future<void> remove(String id) async {
    await ensureLoaded();
    _entries.removeWhere((e) => e.id == id);
    await _persist();
  }

  Future<void> clear() async {
    await ensureLoaded();
    _entries = [];
    await _persist();
  }

  void _prune() {
    _entries.sort((a, b) => b.startedAt.compareTo(a.startedAt));
    if (_entries.length > maxEntries) {
      _entries = _entries.sublist(0, maxEntries);
    }
  }

  Future<void> _persist() async {
    try {
      final file = File(await _path());
      await file.parent.create(recursive: true);
      await file.writeAsString(
        jsonEncode(_entries.map((e) => e.toJson()).toList()),
      );
    } catch (e) {
      log('CallHistoryStore persist error: $e');
    }
  }
}

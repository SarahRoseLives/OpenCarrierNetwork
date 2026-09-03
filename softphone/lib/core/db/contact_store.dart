import 'dart:convert';
import 'dart:developer';
import 'dart:io';
import 'package:path/path.dart' as p;
import 'contact.dart';
import 'storage_paths.dart';

/// Persistent contact list backed by a JSON file.
///
/// Mutations update an in-memory list immediately and serialize a write to
/// disk afterwards, so callers never need to await for the UI to be fresh.
class ContactStore {
  /// Injectable for tests; defaults to the app data directory.
  ContactStore({Future<String> Function()? baseDir})
    : _baseDir = baseDir ?? appDataBaseDir;

  final Future<String> Function() _baseDir;
  List<Contact> _contacts = [];
  bool _loaded = false;

  List<Contact> get items => List.unmodifiable(_contacts);

  Future<String> _path() async => p.join(await _baseDir(), 'ocn_contacts.json');

  Future<void> ensureLoaded() async {
    if (_loaded) return;
    _loaded = true;
    try {
      final file = File(await _path());
      if (!await file.exists()) return;
      final data = await file.readAsString();
      if (data.trim().isEmpty) return;
      final raw = jsonDecode(data) as List<dynamic>;
      _contacts = raw
          .map((e) => Contact.fromJson((e as Map).cast<String, dynamic>()))
          .toList();
      log('ContactStore: loaded ${_contacts.length} contacts');
    } catch (e) {
      log('ContactStore load error: $e');
    }
  }

  Contact? byNumber(String number) {
    if (number.isEmpty) return null;
    // Exact canonical match first, then allow a 7-digit contact to match the
    // local part of a full number (same exchange).
    for (final c in _contacts) {
      if (c.number == number) return c;
    }
    if (number.length == 10) {
      final local = number.substring(3);
      for (final c in _contacts) {
        if (c.number == local) return c;
      }
    }
    return null;
  }

  Future<void> add(Contact contact) async {
    await ensureLoaded();
    _contacts.add(contact);
    await _persist();
  }

  Future<void> update(Contact contact) async {
    await ensureLoaded();
    final i = _contacts.indexWhere((c) => c.id == contact.id);
    if (i < 0) return;
    _contacts[i] = contact;
    await _persist();
  }

  Future<void> remove(String id) async {
    await ensureLoaded();
    _contacts.removeWhere((c) => c.id == id);
    await _persist();
  }

  Future<void> _persist() async {
    try {
      final file = File(await _path());
      await file.parent.create(recursive: true);
      await file.writeAsString(
        jsonEncode(_contacts.map((c) => c.toJson()).toList()),
      );
    } catch (e) {
      log('ContactStore persist error: $e');
    }
  }
}

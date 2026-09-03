import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/core/db/call_history_store.dart';
import 'package:ocnphone/core/db/call_log_entry.dart';
import 'package:ocnphone/core/db/contact.dart';
import 'package:ocnphone/core/db/contact_store.dart';

Future<String> tmpDir() async {
  final d = await Directory.systemTemp.createTemp('ocn_store_test');
  return d.path;
}

void main() {
  group('ContactStore', () {
    test('adds, finds, updates, and removes', () async {
      final dir = await tmpDir();
      final store = ContactStore(baseDir: () async => dir);
      await store.add(_contact(id: '1', name: 'Alice', number: '4405551234'));
      await store.add(_contact(id: '2', name: 'Bob', number: '4405559999'));

      expect(store.items.length, 2);
      expect(store.byNumber('4405551234')?.name, 'Alice');

      // A new store pointing at the same dir sees persisted data.
      final store2 = ContactStore(baseDir: () async => dir);
      await store2.ensureLoaded();
      expect(store2.items.length, 2);

      await store.update(
        _contact(id: '1', name: 'Alice A.', number: '4405551234'),
      );
      expect(store.items.firstWhere((c) => c.id == '1').name, 'Alice A.');

      await store.remove('2');
      expect(store.items.length, 1);
    });

    test('a 7-digit contact matches the local part of a full number', () async {
      final dir = await tmpDir();
      final store = ContactStore(baseDir: () async => dir);
      await store.add(_contact(id: '1', name: 'Local', number: '5551234'));
      expect(store.byNumber('4405551234')?.name, 'Local');
    });
  });

  group('CallHistoryStore', () {
    test('round-trips and sorts newest first', () async {
      final dir = await tmpDir();
      final store = CallHistoryStore(baseDir: () async => dir);
      await store.add(_entry(
        id: 'old',
        number: '4405551234',
        startedAt: DateTime(2026, 1, 1, 10),
      ));
      await store.add(_entry(
        id: 'new',
        number: '4405559999',
        startedAt: DateTime(2026, 1, 2, 10),
      ));

      expect(store.items.map((e) => e.id).toList(), ['new', 'old']);

      final reloaded = CallHistoryStore(baseDir: () async => dir);
      await reloaded.ensureLoaded();
      expect(reloaded.items.length, 2);
    });

    test('update mutates an entry in place', () async {
      final dir = await tmpDir();
      final store = CallHistoryStore(baseDir: () async => dir);
      await store.add(_entry(id: '1', number: '4405551234'));
      await store.update('1', (e) {
        e.connectedAt = DateTime(2026, 1, 1, 10, 0, 30);
        e.durationSec = 30;
      });
      final entry = store.find('1');
      expect(entry, isNotNull);
      expect(entry!.connectedAt, isNotNull);
      expect(entry.durationSec, 30);
    });

    test('remove and clear', () async {
      final dir = await tmpDir();
      final store = CallHistoryStore(baseDir: () async => dir);
      await store.add(_entry(id: '1', number: '4405551234'));
      await store.add(_entry(id: '2', number: '4405559999'));
      await store.remove('1');
      expect(store.items.length, 1);
      await store.clear();
      expect(store.items, isEmpty);
    });

    test('prunes to the entry cap', () async {
      final dir = await tmpDir();
      final store = CallHistoryStore(baseDir: () async => dir);
      final many = CallHistoryStore.maxEntries + 20;
      for (var i = 0; i < many; i++) {
        await store.add(_entry(
          id: 'e$i',
          number: '4405551234',
          startedAt: DateTime(2026, 1, 1).add(Duration(minutes: i)),
        ));
      }
      expect(store.items.length, CallHistoryStore.maxEntries);
    });
  });
}

Contact _contact({
  required String id,
  required String name,
  required String number,
}) {
  return Contact(id: id, name: name, number: number, createdAt: 0);
}

CallLogEntry _entry({
  required String id,
  required String number,
  DateTime? startedAt,
}) {
  return CallLogEntry(
    id: id,
    type: CallType.outgoing,
    number: number,
    startedAt: startedAt ?? DateTime(2026, 1, 1),
  );
}

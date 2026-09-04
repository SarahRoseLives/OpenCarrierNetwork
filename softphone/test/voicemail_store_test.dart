import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/core/db/voicemail_message.dart';
import 'package:ocnphone/core/db/voicemail_store.dart';

Future<String> tmpDir() async {
  final d = await Directory.systemTemp.createTemp('ocn_vm_test');
  return d.path;
}

VoicemailMessage msg(String id, {bool listened = false, int at = 0}) =>
    VoicemailMessage(
      id: id,
      callerNumber: '4405551234',
      callerName: 'Alice',
      durationSeconds: 10,
      listened: listened,
      createdAt: DateTime.fromMillisecondsSinceEpoch(at * 1000),
    );

void main() {
  test('round-trips via json file', () async {
    final dir = await tmpDir();
    final store = VoicemailStore(baseDir: () async => dir);
    await store.ensureLoaded();
    store.replaceAll([msg('a', at: 100), msg('b', listened: true, at: 200)]);
    await store.persist();

    final reloaded = VoicemailStore(baseDir: () async => dir);
    await reloaded.ensureLoaded();
    expect(reloaded.items.length, 2);
    expect(reloaded.unread, 1);
  });

  test('markRead reduces the unread count', () async {
    final dir = await tmpDir();
    final store = VoicemailStore(baseDir: () async => dir);
    await store.ensureLoaded();
    store.replaceAll([msg('a'), msg('b')]);
    expect(store.unread, 2);
    store.markRead('a');
    expect(store.unread, 1);
    expect(store.byId('a')!.listened, isTrue);
  });

  test('upsert and remove', () async {
    final dir = await tmpDir();
    final store = VoicemailStore(baseDir: () async => dir);
    await store.ensureLoaded();
    store.upsert(msg('a'));
    store.upsert(msg('b'));
    store.upsert(msg('a', at: 300)); // replaces existing a
    expect(store.items.length, 2);
    store.remove('a');
    expect(store.items.length, 1);
    expect(store.byId('b'), isNotNull);
  });
}

import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/core/db/dm_message.dart';
import 'package:ocnphone/core/db/dm_store.dart';

Future<String> tmpDir() async {
  final d = await Directory.systemTemp.createTemp('ocn_dm_test');
  return d.path;
}

DmMessage outbound(String id, String peer, String text, {String? clientId}) =>
    DmMessage(
      id: id,
      clientId: clientId ?? id,
      peer: peer,
      direction: 'out',
      kind: 'text',
      text: text,
      status: 'sending',
      createdAt: DateTime.fromMillisecondsSinceEpoch(1000),
    );

DmMessage inbound(String id, String peer, {bool read = false}) => DmMessage(
  id: id,
  peer: peer,
  direction: 'in',
  kind: 'text',
  text: 'hi',
  status: 'delivered',
  read: read,
  createdAt: DateTime.fromMillisecondsSinceEpoch(2000),
);

void main() {
  test('round-trips via json and derives threads/unread', () async {
    final dir = await tmpDir();
    final store = DmStore(baseDir: () async => dir);
    await store.ensureLoaded();
    store.addOrUpdate(outbound('o1', '4405551234', 'hello'));
    store.addOrUpdate(inbound('i1', '4405551234'));
    store.addOrUpdate(inbound('i2', '4405559999'));
    await store.persist();

    final reloaded = DmStore(baseDir: () async => dir);
    await reloaded.ensureLoaded();
    expect(reloaded.items.length, 3);
    expect(reloaded.unreadTotal, 2);
    expect(reloaded.threads.length, 2);

    reloaded.markPeerRead('4405551234');
    expect(reloaded.unreadFor('4405551234'), 0);
    expect(reloaded.unreadTotal, 1);
  });

  test('addOrUpdate dedupes by id', () async {
    final dir = await tmpDir();
    final store = DmStore(baseDir: () async => dir);
    await store.ensureLoaded();
    expect(store.addOrUpdate(inbound('x', '4405551234')), isTrue);
    expect(store.addOrUpdate(inbound('x', '4405551234')), isFalse);
    expect(store.items.length, 1);
  });

  test('messagesFor sorts ascending by time', () async {
    final dir = await tmpDir();
    final store = DmStore(baseDir: () async => dir);
    await store.ensureLoaded();
    final a = DmMessage(
      id: 'a', peer: '4405551234', direction: 'in', kind: 'text', text: 'a',
      createdAt: DateTime.fromMillisecondsSinceEpoch(10),
    );
    final b = DmMessage(
      id: 'b', peer: '4405551234', direction: 'out', kind: 'text', text: 'b',
      createdAt: DateTime.fromMillisecondsSinceEpoch(50),
    );
    store.addOrUpdate(b);
    store.addOrUpdate(a);
    final msgs = store.messagesFor('4405551234');
    expect(msgs.map((m) => m.id).toList(), ['a', 'b']);
  });
}

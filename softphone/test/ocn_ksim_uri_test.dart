import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/core/provision/ocn_ksim_uri.dart';

void main() {
  test('parses the exact URL shape the admin panel emits', () {
    final raw =
        'ocnksim://127.0.0.1/?name=Rose+test&server=ws%3A%2F%2F127.0.0.1%3A9100%2Fws&token=-RJRQTCGyi2tpCn5Su2JTb9WwIXTo95OQcZngUANok4';
    final p = OcnKsimUri.parse(raw);
    expect(p, isNotNull);
    expect(p!.serverUrl, 'ws://127.0.0.1:9100/ws');
    expect(p.token, '-RJRQTCGyi2tpCn5Su2JTb9WwIXTo95OQcZngUANok4');
    expect(p.displayName, 'Rose test');
  });

  test('rejects non-provisioning strings', () {
    expect(OcnKsimUri.parse('https://example.com'), isNull);
    expect(OcnKsimUri.parse('ocnksim://host'), isNull);
    expect(OcnKsimUri.parse('ocnksim://h/?server=ftp://x'), isNull);
  });

  test('encode round-trips', () {
    const p = OcnKsimUri(
      serverUrl: 'ws://192.168.1.240:9100/ws',
      token: 'abc123',
      displayName: 'Rose',
    );
    final p2 = OcnKsimUri.parse(p.encode());
    expect(p2, isNotNull);
    expect(p2!.serverUrl, p.serverUrl);
    expect(p2.token, p.token);
  });
}

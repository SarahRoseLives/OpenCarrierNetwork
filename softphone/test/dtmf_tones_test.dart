import 'dart:typed_data';
import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/core/audio/dtmf_tones.dart';

void main() {
  test('buildDtmfWav produces a valid RIFF/WAVE header', () {
    final wav = buildDtmfWav('5', ms: 100, sampleRate: 22050);
    expect(wav.length, greaterThan(44));

    String ascii(int start, int len) =>
        String.fromCharCodes(wav.sublist(start, start + len));
    expect(ascii(0, 4), 'RIFF');
    expect(ascii(8, 4), 'WAVE');
    expect(ascii(12, 4), 'fmt ');
    expect(ascii(36, 4), 'data');

    final bd = ByteData.sublistView(wav);
    expect(bd.getUint16(20, Endian.little), 1); // PCM
    expect(bd.getUint16(22, Endian.little), 1); // mono
    expect(bd.getUint32(24, Endian.little), 22050);
    expect(bd.getUint16(34, Endian.little), 16); // bits

    // Expected PCM length for 100ms at 22050Hz mono 16-bit.
    final expectedPcm = 22050 * 100 ~/ 1000 * 2;
    expect(wav.length, 44 + expectedPcm);
  });

  test('every DTMF key produces audible (non-silent) PCM', () {
    for (final key in dtmfFrequencies.keys) {
      final wav = buildDtmfWav(key);
      // Skip the 44-byte header; sum absolute amplitude.
      var total = 0;
      final bd = ByteData.sublistView(wav, 44);
      for (var i = 0; i < bd.lengthInBytes ~/ 2; i += 2) {
        total += bd.getInt16(i, Endian.little).abs();
      }
      expect(total, greaterThan(0), reason: 'key "$key" should have sound');
    }
  });

  test('non-DTMF keys are rejected', () {
    expect(() => buildDtmfWav('x'), throwsArgumentError);
    expect(isDtmfKey('5'), isTrue);
    expect(isDtmfKey('*'), isTrue);
    expect(isDtmfKey('#'), isTrue);
    expect(isDtmfKey('a'), isFalse);
  });
}

import 'package:flutter_test/flutter_test.dart';
import 'package:ocnphone/core/phone/number_format.dart';

void main() {
  group('canonicalNumber', () {
    test('keeps a 10-digit full number as-is', () {
      expect(canonicalNumber('440-555-1234', ownArea: '440'),
          '4405551234');
      expect(canonicalNumber('4405551234', ownArea: '440'), '4405551234');
    });

    test('promotes a 7-digit local number with the owner area', () {
      expect(canonicalNumber('5551234', ownArea: '440'), '4405551234');
      expect(canonicalNumber('(555) 1234', ownArea: '440'), '4405551234');
    });

    test('leaves a 7-digit number alone when area unknown', () {
      expect(canonicalNumber('5551234', ownArea: ''), '5551234');
      expect(canonicalNumber('5551234'), '5551234');
    });

    test('keeps service codes untouched', () {
      expect(canonicalNumber('*01', ownArea: '440'), '*01');
      expect(canonicalNumber('#123', ownArea: '440'), '#123');
    });

    test('strips whitespace and returns empty for blank input', () {
      expect(canonicalNumber('   '), '');
      expect(canonicalNumber(''), '');
    });
  });

  group('displayNumber', () {
    test('formats 10 and 7 digit numbers', () {
      expect(displayNumber('4405551234'), '440-555-1234');
      expect(displayNumber('5551234'), '555-1234');
    });

    test('returns service codes unchanged', () {
      expect(displayNumber('*01'), '*01');
    });
  });

  group('service helpers', () {
    test('isServiceCode', () {
      expect(isServiceCode('*01'), isTrue);
      expect(isServiceCode('5551234'), isFalse);
    });

    test('dialFromCanonical round-trips a canonical number', () {
      expect(dialFromCanonical('4405551234'), '4405551234');
    });
  });
}
